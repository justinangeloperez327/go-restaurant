package customer

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
var phoneRE = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{6,22}$`)
var countryRE = regexp.MustCompile(`^[A-Z]{2}$`)

func validID(id string) bool       { return uuidRE.MatchString(id) }
func validPhone(phone string) bool { return phone == "" || phoneRE.MatchString(phone) }
func limit(s string, min, max int) bool {
	n := len([]rune(strings.TrimSpace(s)))
	return n >= min && n <= max
}
func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func fail(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, dest any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("expected application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}
func subject(r *http.Request) string {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return ""
	}
	return user.ID
}
func (s Service) allowWrite(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != s.WebOrigin || s.WebOrigin == "" {
		fail(w, http.StatusForbidden, "invalid request origin")
		return false
	}
	return true
}
func databaseError(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505", "23503", "23001":
			fail(w, http.StatusConflict, "related record is invalid or conflicts")
			return
		case "23514", "23502", "22001", "22P02":
			fail(w, http.StatusBadRequest, "invalid fields")
			return
		}
	}
	fail(w, http.StatusServiceUnavailable, "customer data unavailable")
}
func (s Service) Mount(r chi.Router) {
	r.With(s.Auth.Authenticated).Get("/me/profile", s.getProfile)
	r.With(s.Auth.Authenticated).Put("/me/profile", s.putProfile)
	r.With(s.Auth.Authenticated).Get("/me/addresses", s.listAddresses)
	r.With(s.Auth.Authenticated).Post("/me/addresses", s.addAddress)
	r.With(s.Auth.Authenticated).Put("/me/addresses/{addressID}", s.putAddress)
	r.With(s.Auth.Authenticated).Delete("/me/addresses/{addressID}", s.deleteAddress)
	r.With(s.Auth.Authenticated).Get("/cart", s.getCart)
	r.With(s.Auth.Authenticated).Post("/cart/items", s.addCartItem)
	r.With(s.Auth.Authenticated).Put("/cart/items/{itemID}", s.updateCartItem)
	r.With(s.Auth.Authenticated).Delete("/cart/items/{itemID}", s.deleteCartItem)
	r.With(s.Auth.Authenticated).Delete("/cart", s.clearCart)
}

type ProfileInput struct {
	FullName string `json:"full_name"`
	Phone    string `json:"phone"`
}

func (p ProfileInput) valid() bool {
	return limit(p.FullName, 0, 140) && validPhone(p.Phone)
}
func (s Service) getProfile(w http.ResponseWriter, r *http.Request) {
	var result struct {
		FullName string `json:"full_name"`
		Phone    string `json:"phone"`
		Email    string `json:"email"`
	}
	err := s.DB.QueryRow(r.Context(), `SELECT COALESCE(p.full_name,''), COALESCE(p.phone,''),COALESCE(u.email,'')
	FROM users u LEFT JOIN customer_profiles p ON p.user_id=u.id WHERE u.id=$1::uuid AND u.is_active`,
		subject(r)).Scan(&result.FullName, &result.Phone, &result.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 401, "account unavailable")
		return
	}
	if err != nil {
		databaseError(w, err)
		return
	}
	reply(w, 200, result)
}
func (s Service) putProfile(w http.ResponseWriter, r *http.Request) {
	if !s.allowWrite(w, r) {
		return
	}
	var p ProfileInput
	if err := decode(w, r, &p); err != nil || !p.valid() {
		fail(w, 400, "invalid profile")
		return
	}
	_, err := s.DB.Exec(r.Context(), `INSERT INTO customer_profiles(user_id,full_name,phone)
	VALUES($1::uuid,$2,$3) ON CONFLICT(user_id) DO UPDATE
	SET full_name=EXCLUDED.full_name,phone=EXCLUDED.phone,updated_at=now()`,
		subject(r), strings.TrimSpace(p.FullName), strings.TrimSpace(p.Phone))
	if err != nil {
		databaseError(w, err)
		return
	}
	s.getProfile(w, r)
}

type AddressInput struct {
	Label         string `json:"label"`
	RecipientName string `json:"recipient_name"`
	Phone         string `json:"phone"`
	CountryCode   string `json:"country_code"`
	City          string `json:"city"`
	Area          string `json:"area"`
	Street        string `json:"street"`
	Building      string `json:"building"`
	Apartment     string `json:"apartment"`
	Instructions  string `json:"instructions"`
	IsDefault     bool   `json:"is_default"`
}

func (a AddressInput) valid() bool {
	return limit(a.Label, 1, 40) && limit(a.RecipientName, 1, 140) &&
		a.Phone != "" && validPhone(a.Phone) && countryRE.MatchString(a.CountryCode) &&
		limit(a.City, 1, 100) && limit(a.Area, 0, 150) && limit(a.Street, 1, 200) &&
		limit(a.Building, 0, 120) && limit(a.Apartment, 0, 80) && limit(a.Instructions, 0, 500)
}
func (s Service) listAddresses(w http.ResponseWriter, r *http.Request) {
	const sql = `SELECT COALESCE(jsonb_agg(to_jsonb(a)-'user_id'-'created_at'-'updated_at'
		ORDER BY a.is_default DESC,a.created_at,a.id), '[]'::jsonb)
		FROM delivery_addresses a WHERE a.user_id=$1::uuid`
	var bytes []byte
	if err := s.DB.QueryRow(r.Context(), sql, subject(r)).Scan(&bytes); err != nil {
		databaseError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(bytes)
}
func (s Service) mutateAddress(w http.ResponseWriter, r *http.Request, create bool) {
	if !s.allowWrite(w, r) {
		return
	}
	id := chi.URLParam(r, "addressID")
	if !create && !validID(id) {
		fail(w, 400, "invalid address ID")
		return
	}
	var a AddressInput
	if err := decode(w, r, &a); err != nil || !a.valid() {
		fail(w, 400, "invalid address")
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		databaseError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	// Serializing address operations per user prevents two concurrent defaults.
	var actor string
	err = tx.QueryRow(r.Context(), "SELECT id::text FROM users WHERE id=$1::uuid AND is_active FOR UPDATE", subject(r)).Scan(&actor)
	if err != nil {
		databaseError(w, err)
		return
	}
	var count int
	if err = tx.QueryRow(r.Context(), "SELECT count(*) FROM delivery_addresses WHERE user_id=$1::uuid", actor).Scan(&count); err != nil {
		databaseError(w, err)
		return
	}
	if create && count >= 15 {
		fail(w, 409, "maximum of 15 saved addresses")
		return
	}
	if !create {
		var exists bool
		err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM delivery_addresses WHERE user_id=$1::uuid AND id=$2::uuid)", actor, id).Scan(&exists)
		if err != nil {
			databaseError(w, err)
			return
		}
		if !exists {
			fail(w, 404, "address not found")
			return
		}
	}
	setDefault := a.IsDefault || (create && count == 0)
	if setDefault {
		if _, err = tx.Exec(r.Context(), "UPDATE delivery_addresses SET is_default=false WHERE user_id=$1::uuid AND is_default", actor); err != nil {
			databaseError(w, err)
			return
		}
	}
	if create {
		err = tx.QueryRow(r.Context(), `INSERT INTO delivery_addresses
		 (user_id,label,recipient_name,phone,country_code,city,area,street,building,apartment,instructions,is_default)
		 VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id::text`,
			actor, a.Label, a.RecipientName, a.Phone, a.CountryCode, a.City, a.Area, a.Street, a.Building, a.Apartment, a.Instructions, setDefault).Scan(&id)
	} else {
		_, err = tx.Exec(r.Context(), `UPDATE delivery_addresses SET
		 label=$3,recipient_name=$4,phone=$5,country_code=$6,city=$7,area=$8,street=$9,
		 building=$10,apartment=$11,instructions=$12,is_default=$13,updated_at=now()
		 WHERE user_id=$1::uuid AND id=$2::uuid`,
			actor, id, a.Label, a.RecipientName, a.Phone, a.CountryCode, a.City, a.Area, a.Street, a.Building, a.Apartment, a.Instructions, setDefault)
	}
	if err != nil {
		databaseError(w, err)
		return
	}
	// Keep one default even if the former default is explicitly unchecked.
	_, err = tx.Exec(r.Context(), `UPDATE delivery_addresses SET is_default=true
	WHERE id=(SELECT id FROM delivery_addresses WHERE user_id=$1::uuid
	ORDER BY is_default DESC,created_at,id LIMIT 1)
	AND NOT EXISTS(SELECT 1 FROM delivery_addresses WHERE user_id=$1::uuid AND is_default)`, actor)
	if err != nil {
		databaseError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		databaseError(w, err)
		return
	}
	code := 200
	if create {
		code = 201
	}
	reply(w, code, map[string]string{"id": id})
}
func (s Service) addAddress(w http.ResponseWriter, r *http.Request) { s.mutateAddress(w, r, true) }
func (s Service) putAddress(w http.ResponseWriter, r *http.Request) { s.mutateAddress(w, r, false) }
func (s Service) deleteAddress(w http.ResponseWriter, r *http.Request) {
	if !s.allowWrite(w, r) {
		return
	}
	id := chi.URLParam(r, "addressID")
	if !validID(id) {
		fail(w, 400, "invalid address ID")
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		databaseError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var actor string
	if err = tx.QueryRow(r.Context(), "SELECT id::text FROM users WHERE id=$1::uuid AND is_active FOR UPDATE", subject(r)).Scan(&actor); err != nil {
		databaseError(w, err)
		return
	}
	tag, err := tx.Exec(r.Context(), "DELETE FROM delivery_addresses WHERE id=$1::uuid AND user_id=$2::uuid", id, actor)
	if err != nil {
		databaseError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, 404, "address not found")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE delivery_addresses SET is_default=true WHERE id=
	(SELECT id FROM delivery_addresses WHERE user_id=$1::uuid ORDER BY created_at,id LIMIT 1)
	AND NOT EXISTS(SELECT 1 FROM delivery_addresses WHERE user_id=$1::uuid AND is_default)`, actor)
	if err != nil {
		databaseError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		databaseError(w, err)
		return
	}
	w.WriteHeader(204)
}
