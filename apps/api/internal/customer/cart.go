package customer

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

var errUnavailable = errors.New("meal or selected modifiers unavailable")

type CartSelection struct {
	MealID    string   `json:"meal_id"`
	Quantity  int      `json:"quantity"`
	OptionIDs []string `json:"option_ids"`
}
type OptionSummary struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PriceDeltaMinor int64  `json:"price_delta_minor"`
}
type CartLine struct {
	ID             string          `json:"id"`
	MealID         string          `json:"meal_id"`
	Name           string          `json:"name"`
	ImageURL       string          `json:"image_url"`
	Quantity       int             `json:"quantity"`
	BasePriceMinor int64           `json:"base_price_minor"`
	UnitPriceMinor int64           `json:"unit_price_minor"`
	LineTotalMinor int64           `json:"line_total_minor"`
	Options        []OptionSummary `json:"options"`
	IsValid        bool            `json:"is_valid"`
	Issue          string          `json:"issue,omitempty"`
}
type CartView struct {
	Restaurant *struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Currency string `json:"currency"`
	} `json:"restaurant"`
	Items         []CartLine `json:"items"`
	SubtotalMinor int64      `json:"subtotal_minor"`
	CanCheckout   bool       `json:"can_checkout"`
}
type group struct {
	ID       string
	Min, Max int
	Count    int
}

func validateSelection(ctx context.Context, tx pgx.Tx, restaurantID, mealID string, optionIDs []string) (int64, []OptionSummary, error) {
	var base int64
	var published bool
	err := tx.QueryRow(ctx, `SELECT m.price_minor,(r.is_active AND c.is_active AND m.is_active AND m.is_available)
		FROM meals m JOIN restaurants r ON r.id=m.restaurant_id
		JOIN menu_categories c ON c.id=m.category_id
		WHERE m.id=$1::uuid AND m.restaurant_id=$2::uuid`, mealID, restaurantID).Scan(&base, &published)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, errUnavailable
	}
	if err != nil {
		return 0, nil, err
	}
	if !published {
		return 0, nil, errUnavailable
	}
	grpRows, err := tx.Query(ctx, `SELECT id::text,min_select,max_select FROM modifier_groups
		WHERE restaurant_id=$1::uuid AND meal_id=$2::uuid ORDER BY id`, restaurantID, mealID)
	if err != nil {
		return 0, nil, err
	}
	groups := map[string]*group{}
	for grpRows.Next() {
		g := &group{}
		if err := grpRows.Scan(&g.ID, &g.Min, &g.Max); err != nil {
			grpRows.Close()
			return 0, nil, err
		}
		groups[g.ID] = g
	}
	err = grpRows.Err()
	grpRows.Close()
	if err != nil {
		return 0, nil, err
	}

	options := make([]OptionSummary, 0, len(optionIDs))
	unit := base
	if len(optionIDs) > 0 {
		rows, err := tx.Query(ctx, `SELECT o.id::text,o.group_id::text,o.name,o.price_delta_minor,o.is_available
			FROM modifier_options o
			JOIN modifier_groups g ON g.id=o.group_id AND g.restaurant_id=o.restaurant_id
			WHERE o.restaurant_id=$1::uuid AND g.meal_id=$2::uuid AND o.id::text=ANY($3::text[])
			ORDER BY o.id`, restaurantID, mealID, optionIDs)
		if err != nil {
			return 0, nil, err
		}
		for rows.Next() {
			var opt OptionSummary
			var groupID string
			var available bool
			if err := rows.Scan(&opt.ID, &groupID, &opt.Name, &opt.PriceDeltaMinor, &available); err != nil {
				rows.Close()
				return 0, nil, err
			}
			if !available {
				rows.Close()
				return 0, nil, errUnavailable
			}
			g := groups[groupID]
			if g == nil {
				rows.Close()
				return 0, nil, errUnavailable
			}
			g.Count++
			unit += opt.PriceDeltaMinor
			options = append(options, opt)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, nil, err
		}
	}
	if len(options) != len(optionIDs) {
		return 0, nil, errUnavailable
	}
	for _, g := range groups {
		if g.Count < g.Min || g.Count > g.Max {
			return 0, nil, errUnavailable
		}
	}
	return unit, options, nil
}
func validOptions(ids []string) bool {
	if len(ids) > 30 {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !validID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func (s Service) getCart(w http.ResponseWriter, r *http.Request) {
	view, err := s.loadCart(r.Context(), subject(r))
	if err != nil {
		databaseError(w, err)
		return
	}
	reply(w, 200, view)
}
func (s Service) loadCart(ctx context.Context, owner string) (CartView, error) {
	view := CartView{Items: []CartLine{}}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return view, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var restaurantID string
	var name, currency string
	err = tx.QueryRow(ctx, `SELECT c.restaurant_id::text,r.name,r.currency FROM customer_carts c
		JOIN restaurants r ON r.id=c.restaurant_id WHERE c.user_id=$1::uuid`, owner).Scan(&restaurantID, &name, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, nil
	}
	if err != nil {
		return view, err
	}
	view.Restaurant = &struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Currency string `json:"currency"`
	}{ID: restaurantID, Name: name, Currency: currency}
	rows, err := tx.Query(ctx, `SELECT ci.id::text,ci.meal_id::text,ci.quantity,m.name,m.image_url,m.price_minor
		FROM cart_items ci JOIN meals m ON m.id=ci.meal_id AND m.restaurant_id=ci.restaurant_id
		WHERE ci.user_id=$1::uuid ORDER BY ci.created_at,ci.id`, owner)
	if err != nil {
		return view, err
	}
	for rows.Next() {
		var item CartLine
		if err := rows.Scan(&item.ID, &item.MealID, &item.Quantity, &item.Name, &item.ImageURL, &item.BasePriceMinor); err != nil {
			rows.Close()
			return view, err
		}
		item.Options = []OptionSummary{}
		view.Items = append(view.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return view, err
	}
	view.CanCheckout = len(view.Items) > 0
	for i := range view.Items {
		item := &view.Items[i]
		rows, err := tx.Query(ctx, "SELECT option_id::text FROM cart_item_options WHERE cart_item_id=$1::uuid ORDER BY option_id", item.ID)
		if err != nil {
			return view, err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return view, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return view, err
		}
		unit, options, err := validateSelection(ctx, tx, restaurantID, item.MealID, ids)
		if errors.Is(err, errUnavailable) {
			item.Issue = "Meal or selected options changed. Remove this item and add it again."
			view.CanCheckout = false
			continue
		}
		if err != nil {
			return view, err
		}
		item.UnitPriceMinor = unit
		item.LineTotalMinor = unit * int64(item.Quantity)
		item.Options = options
		item.IsValid = true
		view.SubtotalMinor += item.LineTotalMinor
	}
	if err = tx.Commit(ctx); err != nil {
		return view, err
	}
	return view, nil
}

func (s Service) lockCart(ctx context.Context, tx pgx.Tx, owner string) (string, error) {
	var restaurantID string
	err := tx.QueryRow(ctx, `SELECT restaurant_id::text FROM customer_carts
		WHERE user_id=$1::uuid FOR UPDATE`, owner).Scan(&restaurantID)
	return restaurantID, err
}
func (s Service) addCartItem(w http.ResponseWriter, r *http.Request) {
	if !s.allowWrite(w, r) {
		return
	}
	var input CartSelection
	if err := decode(w, r, &input); err != nil || !validID(input.MealID) ||
		input.Quantity < 1 || input.Quantity > 50 || !validOptions(input.OptionIDs) {
		fail(w, 400, "invalid cart item")
		return
	}
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		databaseError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	owner := subject(r)
	var rid string
	err = tx.QueryRow(ctx, "SELECT restaurant_id::text FROM meals WHERE id=$1::uuid", input.MealID).Scan(&rid)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 409, "meal unavailable")
		return
	}
	if err != nil {
		databaseError(w, err)
		return
	}
	// The first writer establishes the cart's restaurant. Concurrent requests
	// serialize on the user-owned cart row and cannot mix restaurants.
	_, err = tx.Exec(ctx, `INSERT INTO customer_carts(user_id,restaurant_id) VALUES($1::uuid,$2::uuid)
		ON CONFLICT(user_id) DO NOTHING`, owner, rid)
	if err != nil {
		databaseError(w, err)
		return
	}
	current, err := s.lockCart(ctx, tx, owner)
	if err != nil {
		databaseError(w, err)
		return
	}
	if current != rid {
		fail(w, 409, "cart belongs to another restaurant; clear it first")
		return
	}
	if _, _, err = validateSelection(ctx, tx, rid, input.MealID, input.OptionIDs); err != nil {
		if errors.Is(err, errUnavailable) {
			fail(w, 409, "meal or modifier selection unavailable")
			return
		}
		databaseError(w, err)
		return
	}
	var lines, total int
	err = tx.QueryRow(ctx, "SELECT count(*),COALESCE(sum(quantity),0) FROM cart_items WHERE user_id=$1::uuid", owner).Scan(&lines, &total)
	if err != nil {
		databaseError(w, err)
		return
	}
	if lines >= 40 || total+input.Quantity > 100 {
		fail(w, 409, "cart limit exceeded")
		return
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO cart_items(user_id,restaurant_id,meal_id,quantity)
		VALUES($1::uuid,$2::uuid,$3::uuid,$4) RETURNING id::text`, owner, rid, input.MealID, input.Quantity).Scan(&id)
	if err != nil {
		databaseError(w, err)
		return
	}
	for _, optionID := range input.OptionIDs {
		_, err = tx.Exec(ctx, `INSERT INTO cart_item_options(cart_item_id,restaurant_id,option_id)
			VALUES($1::uuid,$2::uuid,$3::uuid)`, id, rid, optionID)
		if err != nil {
			databaseError(w, err)
			return
		}
	}
	if err = tx.Commit(ctx); err != nil {
		databaseError(w, err)
		return
	}
	reply(w, 201, map[string]string{"id": id})
}
func (s Service) updateCartItem(w http.ResponseWriter, r *http.Request) {
	if !s.allowWrite(w, r) {
		return
	}
	id := chi.URLParam(r, "itemID")
	if !validID(id) {
		fail(w, 400, "invalid cart item ID")
		return
	}
	var input struct {
		Quantity int `json:"quantity"`
	}
	if err := decode(w, r, &input); err != nil || input.Quantity < 1 || input.Quantity > 50 {
		fail(w, 400, "invalid quantity")
		return
	}
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		databaseError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	owner := subject(r)
	if _, err = s.lockCart(ctx, tx, owner); errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "cart not found")
		return
	}
	if err != nil {
		databaseError(w, err)
		return
	}
	var oldQty int
	err = tx.QueryRow(ctx, "SELECT quantity FROM cart_items WHERE id=$1::uuid AND user_id=$2::uuid", id, owner).Scan(&oldQty)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "cart item not found")
		return
	}
	if err != nil {
		databaseError(w, err)
		return
	}
	var current int
	err = tx.QueryRow(ctx, "SELECT COALESCE(sum(quantity),0) FROM cart_items WHERE user_id=$1::uuid", owner).Scan(&current)
	if err != nil {
		databaseError(w, err)
		return
	}
	if current-oldQty+input.Quantity > 100 {
		fail(w, 409, "cart limit exceeded")
		return
	}
	_, err = tx.Exec(ctx, "UPDATE cart_items SET quantity=$3 WHERE id=$1::uuid AND user_id=$2::uuid", id, owner, input.Quantity)
	if err != nil {
		databaseError(w, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		databaseError(w, err)
		return
	}
	reply(w, 200, map[string]any{"id": id, "quantity": input.Quantity})
}
func (s Service) deleteCartItem(w http.ResponseWriter, r *http.Request) {
	if !s.allowWrite(w, r) {
		return
	}
	id := chi.URLParam(r, "itemID")
	if !validID(id) {
		fail(w, 400, "invalid cart item ID")
		return
	}
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		databaseError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	owner := subject(r)
	if _, err = s.lockCart(ctx, tx, owner); errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "cart not found")
		return
	}
	if err != nil {
		databaseError(w, err)
		return
	}
	tag, err := tx.Exec(ctx, "DELETE FROM cart_items WHERE id=$1::uuid AND user_id=$2::uuid", id, owner)
	if err != nil {
		databaseError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, 404, "cart item not found")
		return
	}
	// Removing the final item releases the cart's restaurant selection.
	_, err = tx.Exec(ctx, `DELETE FROM customer_carts WHERE user_id=$1::uuid
		AND NOT EXISTS(SELECT 1 FROM cart_items WHERE user_id=$1::uuid)`, owner)
	if err != nil {
		databaseError(w, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		databaseError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s Service) clearCart(w http.ResponseWriter, r *http.Request) {
	if !s.allowWrite(w, r) {
		return
	}
	_, err := s.DB.Exec(r.Context(), "DELETE FROM customer_carts WHERE user_id=$1::uuid", subject(r))
	if err != nil {
		databaseError(w, err)
		return
	}
	w.WriteHeader(204)
}

 // PriceSelection shares the cart's canonical modifier and availability checks with checkout.
 // Callers must use the same PostgreSQL transaction as their order snapshot.
func PriceSelection(ctx context.Context, tx pgx.Tx, restaurantID, mealID string, optionIDs []string) (int64, []OptionSummary, error) {
	return validateSelection(ctx, tx, restaurantID, mealID, optionIDs)
}
func IsUnavailable(err error) bool { return errors.Is(err, errUnavailable) }
