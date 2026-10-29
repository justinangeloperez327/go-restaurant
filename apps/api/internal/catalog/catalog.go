package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/auth"
)

type Service struct {
	DB        *pgxpool.Pool
	Auth      *auth.Service
	WebOrigin string
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var currencyRE = regexp.MustCompile(`^[A-Z]{3}$`)

func validID(id string) bool { return uuidRE.MatchString(id) }
func validText(s string, max int) bool {
	return len(strings.TrimSpace(s)) > 0 && len([]rune(s)) <= max
}
func validImage(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 2048 {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}

func respond(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, code int, message string) {
	respond(w, code, map[string]string{"error": message})
}
func parse(w http.ResponseWriter, r *http.Request, into any) error {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return errors.New("only one JSON object is allowed")
	}
	return nil
}
func (s Service) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin != "" && (origin == s.WebOrigin)
}
func (s Service) actor(r *http.Request) string {
	u, ok := auth.UserFromContext(r.Context())
	if !ok {
		return ""
	}
	return u.ID
}
func postgresFailure(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			failure(w, http.StatusConflict, "record already exists")
			return
		case "23503":
			failure(w, http.StatusConflict, "referenced record does not exist or is still in use")
			return
		case "23502", "23514", "22001":
			failure(w, http.StatusBadRequest, "invalid data")
			return
		}
	}
	failure(w, http.StatusServiceUnavailable, "catalog database unavailable")
}

// Checked within each SQL mutation: permissions and restaurant assignment are
// not assumed from stale browser roles or a previous HTTP authorization check.
const manageScope = `EXISTS (
	SELECT 1 FROM users u
	JOIN user_roles ur ON ur.user_id=u.id
	JOIN role_permissions rp ON rp.role_name=ur.role_name
	WHERE u.id=$2::uuid AND u.is_active AND rp.permission_name='catalog.manage'
	AND (ur.role_name='admin' OR
		(ur.role_name='restaurant_manager' AND EXISTS (
			SELECT 1 FROM restaurant_staff st
			WHERE st.restaurant_id=$1::uuid AND st.user_id=u.id
		)))
)`
const adminScope = `EXISTS (
	SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id=u.id
	JOIN role_permissions rp ON rp.role_name=ur.role_name
	WHERE u.id=$2::uuid AND u.is_active
	AND ur.role_name='admin' AND rp.permission_name='restaurants.manage'
)`

func (s Service) Mount(r chi.Router) {
	r.Get("/restaurants", s.publicRestaurants)
	r.Get("/restaurants/{restaurantID}/menu", s.publicMenu)

	r.Route("/admin/restaurants", func(r chi.Router) {
		r.Use(s.Auth.Authenticated)
		r.Get("/", s.adminRestaurants)
		r.Post("/", s.createRestaurant)
		r.Get("/{restaurantID}/catalog", s.adminCatalog)
		r.Put("/{restaurantID}", s.updateRestaurant)
		r.Delete("/{restaurantID}", s.archiveRestaurant)
		r.Put("/{restaurantID}/managers/{userID}", s.assignManager)
		r.Delete("/{restaurantID}/managers/{userID}", s.removeManager)

		r.Post("/{restaurantID}/categories/", s.edit("categories", ""))
		r.Put("/{restaurantID}/categories/{categoryID}", s.edit("categories", "categoryID"))
		r.Delete("/{restaurantID}/categories/{categoryID}", s.remove("categories", "categoryID"))
		r.Post("/{restaurantID}/meals/", s.edit("meals", ""))
		r.Put("/{restaurantID}/meals/{mealID}", s.edit("meals", "mealID"))
		r.Delete("/{restaurantID}/meals/{mealID}", s.remove("meals", "mealID"))
		r.Post("/{restaurantID}/meals/{mealID}/modifier-groups/", s.edit("groups", ""))
		r.Put("/{restaurantID}/meals/{mealID}/modifier-groups/{groupID}", s.edit("groups", "groupID"))
		r.Delete("/{restaurantID}/meals/{mealID}/modifier-groups/{groupID}", s.remove("groups", "groupID"))
		r.Post("/{restaurantID}/meals/{mealID}/modifier-groups/{groupID}/options/", s.edit("options", ""))
		r.Put("/{restaurantID}/meals/{mealID}/modifier-groups/{groupID}/options/{optionID}", s.edit("options", "optionID"))
		r.Delete("/{restaurantID}/meals/{mealID}/modifier-groups/{groupID}/options/{optionID}", s.remove("options", "optionID"))
	})
}
func (s Service) mustWrite(w http.ResponseWriter, r *http.Request) bool {
	if !s.checkOrigin(r) {
		failure(w, http.StatusForbidden, "invalid origin")
		return false
	}
	return true
}

type restaurantInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Currency    string `json:"currency"`
	ImageURL    string `json:"image_url"`
	IsActive    bool   `json:"is_active"`
}
func (p restaurantInput) valid() bool {
	return validText(p.Name, 140) && len(p.Slug) <= 100 && slugRE.MatchString(p.Slug) &&
		currencyRE.MatchString(p.Currency) && len([]rune(p.Description)) <= 3000 && validImage(p.ImageURL)
}
func (s Service) createRestaurant(w http.ResponseWriter, r *http.Request) {
	if !s.mustWrite(w, r) {
		return
	}
	var input restaurantInput
	if parse(w, r, &input) != nil || !input.valid() {
		failure(w, http.StatusBadRequest, "invalid restaurant details")
		return
	}
	var id string
	err := s.DB.QueryRow(r.Context(), `INSERT INTO restaurants
	(name,slug,description,currency,image_url,is_active)
	SELECT $3,$4,$5,$6,$7,$8 WHERE $1::uuid IS NOT NULL AND `+adminScope+` RETURNING id::text`,
		"00000000-0000-0000-0000-000000000000", s.actor(r), strings.TrimSpace(input.Name), input.Slug, input.Description,
		input.Currency, input.ImageURL, input.IsActive).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		failure(w, http.StatusForbidden, "administrator required")
		return
	}
	if err != nil {
		postgresFailure(w, err)
		return
	}
	respond(w, http.StatusCreated, map[string]string{"id": id})
}
func (s Service) updateRestaurant(w http.ResponseWriter, r *http.Request) {
	if !s.mustWrite(w, r) {
		return
	}
	id := chi.URLParam(r, "restaurantID")
	if !validID(id) {
		failure(w, 400, "invalid restaurant ID")
		return
	}
	var input restaurantInput
	if parse(w, r, &input) != nil || !input.valid() {
		failure(w, 400, "invalid restaurant details")
		return
	}
	var result string
	err := s.DB.QueryRow(r.Context(), `UPDATE restaurants r
	SET name=$3,slug=$4,description=$5,image_url=$6,is_active=$7,updated_at=now()
	WHERE r.id=$1::uuid AND r.currency=$8 AND `+manageScope+` RETURNING r.id::text`,
		id, s.actor(r), strings.TrimSpace(input.Name), input.Slug, input.Description,
		input.ImageURL, input.IsActive, input.Currency).Scan(&result)
	if errors.Is(err, pgx.ErrNoRows) {
		failure(w, 403, "not permitted or currency cannot change")
		return
	}
	if err != nil {
		postgresFailure(w, err)
		return
	}
	respond(w, 200, map[string]string{"id": result})
}
func (s Service) archiveRestaurant(w http.ResponseWriter, r *http.Request) {
	if !s.mustWrite(w, r) {
		return
	}
	id := chi.URLParam(r, "restaurantID")
	if !validID(id) {
		failure(w, 400, "invalid restaurant ID")
		return
	}
	tag, err := s.DB.Exec(r.Context(), `UPDATE restaurants r SET is_active=false, updated_at=now()
	WHERE r.id=$1::uuid AND `+adminScope, id, s.actor(r))
	if err != nil {
		postgresFailure(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		failure(w, 403, "administrator required")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s Service) manager(w http.ResponseWriter, r *http.Request, add bool) {
	if !s.mustWrite(w, r) {
		return
	}
	id, user := chi.URLParam(r, "restaurantID"), chi.URLParam(r, "userID")
	if !validID(id) || !validID(user) {
		failure(w, 400, "invalid ID")
		return
	}
	var tag pgconn.CommandTag
	var err error
	if add {
		tag, err = s.DB.Exec(r.Context(), `INSERT INTO restaurant_staff (restaurant_id,user_id)
		SELECT $1::uuid, $3::uuid
		WHERE `+adminScope+`
		AND EXISTS (SELECT 1 FROM restaurants WHERE id=$1::uuid)
		AND EXISTS (SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id=u.id
			WHERE u.id=$3::uuid AND u.is_active AND ur.role_name='restaurant_manager')
		ON CONFLICT DO NOTHING`, id, s.actor(r), user)
	} else {
		tag, err = s.DB.Exec(r.Context(), `DELETE FROM restaurant_staff st
		WHERE st.restaurant_id=$1::uuid AND st.user_id=$3::uuid
		AND `+adminScope, id, s.actor(r), user)
	}
	if err != nil {
		postgresFailure(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		failure(w, 403, "not permitted or assignment ineligible")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s Service) assignManager(w http.ResponseWriter, r *http.Request) { s.manager(w, r, true) }
func (s Service) removeManager(w http.ResponseWriter, r *http.Request) { s.manager(w, r, false) }

func (s Service) canManage(ctx context.Context, restaurantID, actorID string) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, "SELECT "+manageScope, restaurantID, actorID).Scan(&ok)
	return ok, err
}
func (s Service) adminRestaurants(w http.ResponseWriter, r *http.Request) {
	scope := strings.ReplaceAll(manageScope, "$1::uuid", "r.id")
	var output []byte
	err := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(r)
		ORDER BY r.created_at DESC, r.id), '[]'::jsonb)
		FROM restaurants r WHERE $1::uuid IS NOT NULL AND `+scope, "00000000-0000-0000-0000-000000000000", s.actor(r)).Scan(&output)
	if err != nil {
		postgresFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(output)
}
func (s Service) publicRestaurants(w http.ResponseWriter, r *http.Request) {
	var output []byte
	err := s.DB.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(
		jsonb_build_object('id',id,'name',name,'slug',slug,'description',description,
		'currency',currency,'image_url',image_url) ORDER BY name), '[]'::jsonb)
		FROM restaurants WHERE is_active`).Scan(&output)
	if err != nil {
		postgresFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(output)
}
func (s Service) publicMenu(w http.ResponseWriter, r *http.Request) {
	s.catalog(w, r, false)
}
func (s Service) adminCatalog(w http.ResponseWriter, r *http.Request) {
	s.catalog(w, r, true)
}
func (s Service) catalog(w http.ResponseWriter, r *http.Request, admin bool) {
	rid := chi.URLParam(r, "restaurantID")
	if !validID(rid) {
		failure(w, 400, "invalid restaurant ID")
		return
	}
	if admin {
		ok, err := s.canManage(r.Context(), rid, s.actor(r))
		if err != nil { postgresFailure(w, err); return }
		if !ok { failure(w, 403, "restaurant access denied"); return }
	}
	data, err := s.getCatalog(r.Context(), rid, admin)
	if errors.Is(err, pgx.ErrNoRows) {
		failure(w, 404, "restaurant not found")
		return
	}
	if err != nil {
		postgresFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
func (s Service) getCatalog(ctx context.Context, rid string, admin bool) ([]byte, error) {
	activeRestaurant,activeCategory,activeMeal,activeOption := "", "", "", ""
	if !admin {
		activeRestaurant = " AND r.is_active"
		activeCategory = " AND c.is_active"
		activeMeal = " AND m.is_active"
		activeOption = " AND o.is_available"
	}
	// Bound by one restaurant, with explicit public filters to prevent leaking drafts.
	query := `SELECT jsonb_build_object(
		'restaurant', to_jsonb(r)-'created_at'-'updated_at',
		'categories', COALESCE((SELECT jsonb_agg(to_jsonb(c)-'created_at' ORDER BY c.sort_order,c.id)
		  FROM menu_categories c WHERE c.restaurant_id=r.id`+activeCategory+`),'[]'::jsonb),
		'meals', COALESCE((SELECT jsonb_agg(to_jsonb(m)-'created_at' ORDER BY m.sort_order,m.id)
		  FROM meals m JOIN menu_categories c ON c.id=m.category_id
		  WHERE m.restaurant_id=r.id`+activeMeal+activeCategory+`),'[]'::jsonb),
		'modifier_groups', COALESCE((SELECT jsonb_agg(to_jsonb(g)-'created_at' ORDER BY g.sort_order,g.id)
		  FROM modifier_groups g
		  JOIN meals m ON m.id=g.meal_id JOIN menu_categories c ON c.id=m.category_id
		  WHERE g.restaurant_id=r.id`+activeMeal+activeCategory+`),'[]'::jsonb),
		'modifier_options', COALESCE((SELECT jsonb_agg(to_jsonb(o)-'created_at' ORDER BY o.sort_order,o.id)
		  FROM modifier_options o JOIN modifier_groups g ON g.id=o.group_id
		  JOIN meals m ON m.id=g.meal_id JOIN menu_categories c ON c.id=m.category_id
		  WHERE o.restaurant_id=r.id`+activeMeal+activeCategory+activeOption+`),'[]'::jsonb)
	  ) FROM restaurants r WHERE r.id=$1::uuid`+activeRestaurant
	var output []byte
	err := s.DB.QueryRow(ctx, query, rid).Scan(&output)
	return output, err
}

type spec struct {
	table, parentColumn, parentPath string
	columns []string
}
func resource(kind string) spec {
	switch kind {
	case "categories": return spec{table:"menu_categories", columns:[]string{"name","sort_order","is_active"}}
	case "meals": return spec{table:"meals", columns:[]string{"category_id","name","description","price_minor","image_url","is_active","is_available","sort_order"}}
	case "groups": return spec{table:"modifier_groups",parentColumn:"meal_id",parentPath:"mealID",columns:[]string{"name","min_select","max_select","sort_order"}}
	case "options": return spec{table:"modifier_options",parentColumn:"group_id",parentPath:"groupID",columns:[]string{"name","price_delta_minor","is_available","sort_order"}}
	default: panic("unknown internal catalog resource")
	}
}
func values(w http.ResponseWriter, r *http.Request, kind string) ([]any,bool) {
	switch kind {
	case "categories":
		var x struct { Name string `json:"name"`; SortOrder int `json:"sort_order"`; IsActive bool `json:"is_active"` }
		if parse(w,r,&x)!=nil || !validText(x.Name,100) { return nil,false }
		return []any{strings.TrimSpace(x.Name),x.SortOrder,x.IsActive},true
	case "meals":
		var x struct {
			CategoryID string `json:"category_id"`
			Name string `json:"name"`
			Description string `json:"description"`
			PriceMinor int64 `json:"price_minor"`
			ImageURL string `json:"image_url"`
			IsActive bool `json:"is_active"`
			IsAvailable bool `json:"is_available"`
			SortOrder int `json:"sort_order"`
		}
		if parse(w,r,&x)!=nil || !validID(x.CategoryID) || !validText(x.Name,140) ||
			len([]rune(x.Description))>3000 || x.PriceMinor<0 || x.PriceMinor>1000000000 || !validImage(x.ImageURL) {
			return nil,false
		}
		return []any{x.CategoryID,strings.TrimSpace(x.Name),x.Description,x.PriceMinor,x.ImageURL,x.IsActive,x.IsAvailable,x.SortOrder},true
	case "groups":
		var x struct { Name string `json:"name"`; Min int `json:"min_select"`; Max int `json:"max_select"`; SortOrder int `json:"sort_order"` }
		if parse(w,r,&x)!=nil || !validText(x.Name,100) || x.Min<0 || x.Max<1 || x.Max>30 || x.Min>x.Max { return nil,false }
		return []any{strings.TrimSpace(x.Name),x.Min,x.Max,x.SortOrder},true
	case "options":
		var x struct { Name string `json:"name"`; PriceDeltaMinor int64 `json:"price_delta_minor"`; IsAvailable bool `json:"is_available"`; SortOrder int `json:"sort_order"` }
		if parse(w,r,&x)!=nil || !validText(x.Name,100) || x.PriceDeltaMinor<0 || x.PriceDeltaMinor>1000000000 { return nil,false }
		return []any{strings.TrimSpace(x.Name),x.PriceDeltaMinor,x.IsAvailable,x.SortOrder},true
	}
	return nil,false
}
func (s Service) edit(kind, idPath string) http.HandlerFunc {
	sp := resource(kind)
	return func(w http.ResponseWriter,r *http.Request) {
		if !s.mustWrite(w,r) { return }
		rid, id := chi.URLParam(r,"restaurantID"), ""
		if idPath!="" { id=chi.URLParam(r,idPath) }
		parent:=""
		if sp.parentPath!="" { parent=chi.URLParam(r,sp.parentPath) }
		if !validID(rid) || (idPath!="" && !validID(id)) || (sp.parentPath!="" && !validID(parent)) {
			failure(w,400,"invalid ID"); return
		}
		input,ok := values(w,r,kind)
		if !ok { failure(w,400,"invalid catalog details"); return }
		args:=[]any{rid,s.actor(r)}
		if id!="" { args=append(args,id) }
		if parent!="" { args=append(args,parent) }
		columns := []string{"restaurant_id"}
		params := []string{"$1::uuid"}
		if parent!="" {
			columns=append(columns,sp.parentColumn)
			params=append(params,fmt.Sprintf("$%d::uuid",len(args)))
		}
		columns=append(columns,sp.columns...)
		for _,v:=range input { args=append(args,v); params=append(params,fmt.Sprintf("$%d",len(args))) }
		var query string
		if id=="" {
			query="INSERT INTO "+sp.table+" ("+strings.Join(columns,",")+") SELECT "+strings.Join(params,",")+" WHERE "+manageScope+" RETURNING id::text"
		} else {
			set:=[]string{}
			// Update only user-configurable columns, never restaurant or parent ownership.
			for i,col:=range sp.columns {
				index:=len(args)-len(input)+i+1
				set=append(set,fmt.Sprintf("%s=$%d",col,index))
			}
			query="UPDATE "+sp.table+" x SET "+strings.Join(set,",")+" WHERE x.restaurant_id=$1::uuid AND x.id=$3::uuid"
			if parent!="" { query+=fmt.Sprintf(" AND x.%s=$4::uuid",sp.parentColumn) }
			query+=" AND "+manageScope+" RETURNING x.id::text"
		}
		var saved string
		err:=s.DB.QueryRow(r.Context(),query,args...).Scan(&saved)
		if errors.Is(err,pgx.ErrNoRows) { failure(w,403,"catalog access denied");return }
		if err!=nil { postgresFailure(w,err);return }
		code:=http.StatusOK
		if id=="" {code=http.StatusCreated}
		respond(w,code,map[string]string{"id":saved})
	}
}
func (s Service) remove(kind,idPath string) http.HandlerFunc {
	sp:=resource(kind)
	return func(w http.ResponseWriter,r *http.Request) {
		if !s.mustWrite(w,r) { return }
		rid,id:=chi.URLParam(r,"restaurantID"),chi.URLParam(r,idPath)
		parent:=""
		if sp.parentPath!="" { parent=chi.URLParam(r,sp.parentPath) }
		if !validID(rid)||!validID(id)||(sp.parentPath!=""&&!validID(parent)) {failure(w,400,"invalid ID");return}
		args:=[]any{rid,s.actor(r),id}
		query:="DELETE FROM "+sp.table+" x WHERE x.restaurant_id=$1::uuid AND x.id=$3::uuid"
		if parent!="" {args=append(args,parent);query+=" AND x."+sp.parentColumn+"=$4::uuid"}
		query+=" AND "+manageScope
		tag,err:=s.DB.Exec(r.Context(),query,args...)
		if err!=nil {postgresFailure(w,err);return}
		if tag.RowsAffected()==0 {failure(w,403,"catalog access denied or record not found");return}
		w.WriteHeader(http.StatusNoContent)
	}
}
