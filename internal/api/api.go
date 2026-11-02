package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"schedule/internal/config"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	db  *pgxpool.Pool
	cfg config.Config
	log *slog.Logger
	mux *http.ServeMux
}

func New(db *pgxpool.Pool, c config.Config, l *slog.Logger) http.Handler {
	s := &Server{db, c, l, http.NewServeMux()}
	s.routes()
	return s.logging(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		e := s.db.Ping(r.Context())
		if e != nil {
			fail(w, 503, "database_unavailable", "database unavailable")
			return
		}
		jsonOut(w, 200, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /resources", s.resources)
	s.mux.HandleFunc("POST /resources", s.createResource)
	s.mux.HandleFunc("GET /resources/available", s.availableResources)
	s.mux.HandleFunc("GET /resources/{id}", s.getResource)
	s.mux.HandleFunc("PATCH /resources/{id}", s.patchResource)
	s.mux.HandleFunc("GET /resources/{id}/bookings", s.schedule)
	s.mux.HandleFunc("GET /resources/{id}/availability", s.availability)
	s.mux.HandleFunc("POST /users", s.createUser)
	s.mux.HandleFunc("GET /users/{id}/bookings", s.userBookings)
	s.mux.HandleFunc("POST /bookings", s.createBooking)
	s.mux.HandleFunc("GET /bookings/{id}", s.getBooking)
	s.mux.HandleFunc("POST /bookings/{id}/cancel", s.cancelBooking)
	s.mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "docs/openapi.yaml") })
}

func (s *Server) logging(n http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = fmt.Sprint(time.Now().UnixNano())
		}

		rw := &responseWriter{ResponseWriter: w}
		rw.Header().Set("X-Request-ID", id)
		n.ServeHTTP(rw, r)

		if rw.status >= 400 && rw.status < 500 {
			s.log.Warn("http request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", rw.status, "error", rw.errorMsg, "duration_ms", time.Since(t).Milliseconds())
		} else if rw.status >= 500 {
			s.log.Error("http request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", rw.status, "error", rw.errorMsg, "duration_ms", time.Since(t).Milliseconds())
		} else {
			s.log.Info("http request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", rw.status, "duration_ms", time.Since(t).Milliseconds())
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	status   int
	errorMsg string
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.status == 0 {
		rw.status = code
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.status == 0 {
		rw.status = 200
	}
	return rw.ResponseWriter.Write(b)
}

func jsonOut(w http.ResponseWriter, c int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(c)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, c int, code, msg string) {
	if rw, ok := w.(*responseWriter); ok {
		rw.errorMsg = msg
	}
	jsonOut(w, c, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func bad(w http.ResponseWriter, m string) {
	fail(w, 400, "bad_request", m)
}

func decode(r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

func pid(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func stamp(s string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, s)
	return t.UTC(), e
}

func queryStamp(r *http.Request, k string) (time.Time, error) {
	v := r.URL.Query().Get(k)
	if v == "" {
		return time.Time{}, errors.New("missing " + k)
	}
	return stamp(v)
}

func dberr(w http.ResponseWriter, e error) {
	var p *pgconn.PgError
	if errors.As(e, &p) && p.Code == "23503" {
		fail(w, 404, "not_found", "referenced record not found")
		return
	}
	fail(w, 500, "internal_error", "internal server error")
}

type Resource struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	Capacity    int       `json:"capacity"`
	Active      bool      `json:"is_active"`
	Created     time.Time `json:"created_at"`
}
type User struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	Created time.Time `json:"created_at"`
}
type Booking struct {
	ID         int64      `json:"id"`
	ResourceID int64      `json:"resource_id"`
	UserID     int64      `json:"user_id"`
	Start      time.Time  `json:"start_at"`
	End        time.Time  `json:"end_at"`
	Status     string     `json:"status"`
	Created    time.Time  `json:"created_at"`
	Canceled   *time.Time `json:"canceled_at,omitempty"`
}

const rc = "id,name,type,description,location,capacity,is_active,created_at"
const bc = "id,resource_id,user_id,start_at,end_at,status,created_at,canceled_at"

func sr(row pgx.Row) (x Resource, e error) {
	e = row.Scan(&x.ID, &x.Name, &x.Type, &x.Description, &x.Location, &x.Capacity, &x.Active, &x.Created)
	return
}

func sb(row pgx.Row) (x Booking, e error) {
	e = row.Scan(&x.ID, &x.ResourceID, &x.UserID, &x.Start, &x.End, &x.Status, &x.Created, &x.Canceled)
	return
}

func (s *Server) resources(w http.ResponseWriter, r *http.Request) {

	q := r.URL.Query()
	sql := "SELECT " + rc + " FROM resources WHERE true"

	var a []any
	v := q.Get("type")
	if v != "" {
		a = append(a, v)
		sql += fmt.Sprintf(" AND type=$%d", len(a))
	}

	v = q.Get("active")
	if v != "" {
		b, e := strconv.ParseBool(v)
		if e != nil {
			bad(w, "active must be boolean")
			return
		}
		a = append(a, b)
		sql += fmt.Sprintf(" AND is_active=$%d", len(a))
	}

	rows, e := s.db.Query(r.Context(), sql+" ORDER BY id", a...)
	if e != nil {
		dberr(w, e)
		return
	}

	defer rows.Close()

	var out []Resource
	for rows.Next() {
		x, e := sr(rows)
		if e != nil {
			dberr(w, e)
			return
		}
		out = append(out, x)
	}
	jsonOut(w, 200, out)
}

type resourceInput struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Capacity    int    `json:"capacity"`
	Active      *bool  `json:"is_active"`
}

func (s *Server) createResource(w http.ResponseWriter, r *http.Request) {

	var x resourceInput
	if decode(r, &x) != nil || strings.TrimSpace(x.Name) == "" || strings.TrimSpace(x.Type) == "" {
		bad(w, "name and type are required")
		return
	}
	if x.Capacity == 0 {
		x.Capacity = 1
	}
	if x.Capacity < 1 {
		bad(w, "capacity must be positive")
		return
	}

	a := true
	if x.Active != nil {
		a = *x.Active
	}

	v, e := sr(s.db.QueryRow(r.Context(), "INSERT INTO resources(name,type,description,location,capacity,is_active) VALUES($1,$2,$3,$4,$5,$6) RETURNING "+rc, x.Name, x.Type, x.Description, x.Location, x.Capacity, a))
	if e != nil {
		dberr(w, e)
		return
	}

	jsonOut(w, 201, v)
}

func (s *Server) getResource(w http.ResponseWriter, r *http.Request) {

	id, e := pid(r)
	if e != nil {
		bad(w, "invalid id")
		return
	}

	x, e := sr(s.db.QueryRow(r.Context(), "SELECT "+rc+" FROM resources WHERE id=$1", id))
	if errors.Is(e, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "resource not found")
		return
	}
	if e != nil {
		dberr(w, e)
		return
	}

	jsonOut(w, 200, x)
}
func (s *Server) patchResource(w http.ResponseWriter, r *http.Request) {

	id, e := pid(r)
	if e != nil {
		bad(w, "invalid id")
		return
	}

	var x resourceInput
	if decode(r, &x) != nil {
		bad(w, "invalid JSON")
		return
	}

	if x.Name == "" || x.Type == "" {
		bad(w, "PATCH requires name and type")
		return
	}

	a := true
	if x.Active != nil {
		a = *x.Active
	}

	v, e := sr(s.db.QueryRow(r.Context(), "UPDATE resources SET name=$2,type=$3,description=$4,location=$5,capacity=$6,is_active=$7 WHERE id=$1 RETURNING "+rc, id, x.Name, x.Type, x.Description, x.Location, max(x.Capacity, 1), a))
	if errors.Is(e, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "resource not found")
		return
	}
	if e != nil {
		dberr(w, e)
		return
	}

	jsonOut(w, 200, v)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {

	var x struct {
		Name string `json:"name"`
	}

	if decode(r, &x) != nil || strings.TrimSpace(x.Name) == "" {
		bad(w, "name is required")
		return
	}

	var u User

	e := s.db.QueryRow(r.Context(), "INSERT INTO users(name) VALUES($1) RETURNING id,name,created_at", x.Name).Scan(&u.ID, &u.Name, &u.Created)
	if e != nil {
		dberr(w, e)
		return
	}

	jsonOut(w, 201, u)
}

var overlap = errors.New("overlap")
var missingUser = errors.New("missing user")
var missingResource = errors.New("missing resource")
var inactive = errors.New("inactive")
var notOwner = errors.New("not owner")

func (s *Server) createBooking(w http.ResponseWriter, r *http.Request) {

	var x struct {
		ResourceID int64  `json:"resource_id"`
		UserID     int64  `json:"user_id"`
		Start      string `json:"start_at"`
		End        string `json:"end_at"`
	}

	if decode(r, &x) != nil || x.ResourceID < 1 || x.UserID < 1 {
		bad(w, "resource_id, user_id, start_at and end_at are required")
		return
	}

	a, e := stamp(x.Start)
	if e != nil {
		bad(w, "start_at must be RFC3339")
		return
	}

	b, e := stamp(x.End)
	if e != nil {
		bad(w, "end_at must be RFC3339")
		return
	}

	if !a.Before(b) {
		fail(w, 422, "invalid_interval", "start_at must be before end_at")
		return
	}
	if !a.After(time.Now().UTC()) {
		fail(w, 422, "past_booking", "start_at must be in the future")
		return
	}

	d := b.Sub(a)
	if d < s.cfg.MinDuration || d > s.cfg.MaxDuration {
		fail(w, 422, "invalid_duration", "booking duration is outside configured limits")
		return
	}

	for i := 0; i <= s.cfg.Retries; i++ {

		v, e := s.bookTx(r.Context(), x.ResourceID, x.UserID, a, b)

		if e == nil {
			jsonOut(w, 201, v)
			return
		}
		if errors.Is(e, overlap) {
			fail(w, 409, "booking_conflict", "resource is already booked")
			return
		}
		if errors.Is(e, missingUser) {
			fail(w, 404, "user_not_found", "user not found")
			return
		}
		if errors.Is(e, missingResource) {
			fail(w, 404, "resource_not_found", "resource not found")
			return
		}
		if errors.Is(e, inactive) {
			fail(w, 409, "resource_inactive", "resource is inactive")
			return
		}

		var pe *pgconn.PgError
		if errors.As(e, &pe) && pe.Code == "40001" {
			if i < s.cfg.Retries {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(time.Duration(i+1) * 20 * time.Millisecond):
				}
				continue
			}
			fail(w, 503, "serialization_retries_exhausted", "retry the request")
			return
		}

		if errors.Is(e, context.Canceled) {
			return
		}

		dberr(w, e)
		return
	}
}
func (s *Server) bookTx(ctx context.Context, rid, uid int64, a, b time.Time) (Booking, error) {

	tx, e := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if e != nil {
		return Booking{}, e
	}

	defer tx.Rollback(context.Background())

	var ok bool
	e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)", uid).Scan(&ok)
	if e != nil {
		return Booking{}, e
	}
	if !ok {
		return Booking{}, missingUser
	}

	var active bool
	e = tx.QueryRow(ctx, "SELECT is_active FROM resources WHERE id=$1", rid).Scan(&active)
	if errors.Is(e, pgx.ErrNoRows) {
		return Booking{}, missingResource
	}
	if e != nil {
		return Booking{}, e
	}
	if !active {
		return Booking{}, inactive
	}

	if !a.After(time.Now().UTC()) {
		return Booking{}, errors.New("start time passed")
	}

	e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bookings WHERE resource_id=$1 AND status='active' AND start_at<$3 AND end_at>$2)", rid, a, b).Scan(&ok)
	if e != nil {
		return Booking{}, e
	}
	if ok {
		return Booking{}, overlap
	}

	v, e := sb(tx.QueryRow(ctx, "INSERT INTO bookings(resource_id,user_id,start_at,end_at) VALUES($1,$2,$3,$4) RETURNING "+bc, rid, uid, a, b))
	if e != nil {
		return Booking{}, e
	}

	e = tx.Commit(ctx)
	if e != nil {
		return Booking{}, e
	}

	return v, nil
}

func (s *Server) getBooking(w http.ResponseWriter, r *http.Request) {

	id, e := pid(r)
	if e != nil {
		bad(w, "invalid id")
		return
	}

	b, e := sb(s.db.QueryRow(r.Context(), "SELECT "+bc+" FROM bookings WHERE id=$1", id))
	if errors.Is(e, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "booking not found")
		return
	}
	if e != nil {
		dberr(w, e)
		return
	}

	jsonOut(w, 200, b)
}
func (s *Server) cancelBooking(w http.ResponseWriter, r *http.Request) {

	id, e := pid(r)
	if e != nil {
		bad(w, "invalid id")
		return
	}

	var x struct {
		UserID int64 `json:"user_id"`
	}
	if decode(r, &x) != nil || x.UserID < 1 {
		bad(w, "user_id is required")
		return
	}

	for i := 0; i <= s.cfg.Retries; i++ {
		b, e := s.cancelTx(r.Context(), id, x.UserID)
		if e == nil {
			jsonOut(w, 200, b)
			return
		}
		if errors.Is(e, pgx.ErrNoRows) {
			fail(w, 404, "not_found", "booking not found")
			return
		}
		if errors.Is(e, notOwner) {
			fail(w, 403, "forbidden", "booking belongs to another user")
			return
		}

		var p *pgconn.PgError
		if errors.As(e, &p) && p.Code == "40001" && i < s.cfg.Retries {
			time.Sleep(time.Duration(i+1) * 20 * time.Millisecond)
			continue
		}
		if errors.As(e, &p) && p.Code == "40001" {
			fail(w, 503, "serialization_retries_exhausted", "retry the request")
			return
		}

		dberr(w, e)

		return
	}
}
func (s *Server) cancelTx(ctx context.Context, id, uid int64) (Booking, error) {

	tx, e := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if e != nil {
		return Booking{}, e
	}

	defer tx.Rollback(context.Background())

	b, e := sb(tx.QueryRow(ctx, "SELECT "+bc+" FROM bookings WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return Booking{}, e
	}
	if b.UserID != uid {
		return Booking{}, notOwner
	}

	if b.Status == "active" {
		b, e = sb(tx.QueryRow(ctx, "UPDATE bookings SET status='canceled',canceled_at=now() WHERE id=$1 RETURNING "+bc, id))
		if e != nil {
			return Booking{}, e
		}
	}

	e = tx.Commit(ctx)
	if e != nil {
		return Booking{}, e
	}
	return b, nil
}

func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {

	id, e := pid(r)
	if e != nil {
		bad(w, "invalid id")
		return
	}

	a, e := queryStamp(r, "from")
	if e != nil {
		bad(w, "from RFC3339 required")
		return
	}

	b, e := queryStamp(r, "to")
	if e != nil || !a.Before(b) {
		bad(w, "to must be after from")
		return
	}

	rows, e := s.db.Query(r.Context(), "SELECT "+bc+" FROM bookings WHERE resource_id=$1 AND status='active' AND start_at<$3 AND end_at>$2 ORDER BY start_at", id, a, b)
	if e != nil {
		dberr(w, e)
		return
	}

	defer rows.Close()

	var out []Booking
	for rows.Next() {
		v, e := sb(rows)
		if e != nil {
			dberr(w, e)
			return
		}
		out = append(out, v)
	}
	jsonOut(w, 200, out)
}

type Free struct {
	Start time.Time `json:"start_at"`
	End   time.Time `json:"end_at"`
}

func (s *Server) availability(w http.ResponseWriter, r *http.Request) {

	id, e := pid(r)
	if e != nil {
		bad(w, "invalid id")
		return
	}

	a, e := queryStamp(r, "from")
	if e != nil {
		bad(w, "from required")
		return
	}

	b, e := queryStamp(r, "to")
	if e != nil || !a.Before(b) {
		bad(w, "to must be after from")
		return
	}

	duration := time.Duration(0)
	v := r.URL.Query().Get("duration")

	if v != "" {
		duration, e = time.ParseDuration(v)
		if e != nil || duration <= 0 {
			bad(w, "duration must be positive, e.g. 30m")
			return
		}
	}

	var ok bool
	e = s.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM resources WHERE id=$1 AND is_active)", id).Scan(&ok)
	if e != nil {
		dberr(w, e)
		return
	}
	if !ok {
		fail(w, 404, "resource_not_found", "active resource not found")
		return
	}

	rows, e := s.db.Query(r.Context(), "SELECT start_at,end_at FROM bookings WHERE resource_id=$1 AND status='active' AND start_at<$3 AND end_at>$2 ORDER BY start_at", id, a, b)
	if e != nil {
		dberr(w, e)
		return
	}

	defer rows.Close()

	var out []Free
	cur := a

	for rows.Next() {
		var x, y time.Time
		if e = rows.Scan(&x, &y); e != nil {
			dberr(w, e)
			return
		}

		if x.After(cur) && (duration == 0 || x.Sub(cur) >= duration) {
			out = append(out, Free{cur, x})
		}

		if y.After(cur) {
			cur = y
		}
		if cur.After(b) {
			cur = b
		}
	}

	if cur.Before(b) && (duration == 0 || b.Sub(cur) >= duration) {
		out = append(out, Free{cur, b})
	}

	jsonOut(w, 200, out)
}

func (s *Server) availableResources(w http.ResponseWriter, r *http.Request) {

	a, e := queryStamp(r, "from")
	if e != nil {
		bad(w, "from required")
		return
	}

	b, e := queryStamp(r, "to")
	if e != nil || !a.Before(b) {
		bad(w, "to must be after from")
		return
	}

	rows, e := s.db.Query(r.Context(), "SELECT "+rc+" FROM resources r WHERE is_active AND NOT EXISTS(SELECT 1 FROM bookings b WHERE b.resource_id=r.id AND b.status='active' AND b.start_at<$2 AND b.end_at>$1) ORDER BY id", a, b)
	if e != nil {
		dberr(w, e)
		return
	}

	defer rows.Close()

	var out []Resource

	for rows.Next() {

		v, e := sr(rows)
		if e != nil {
			dberr(w, e)
			return
		}

		out = append(out, v)
	}
	jsonOut(w, 200, out)
}

func (s *Server) userBookings(w http.ResponseWriter, r *http.Request) {

	id, e := pid(r)
	if e != nil {
		bad(w, "invalid id")
		return
	}

	q := r.URL.Query()
	sql := "SELECT " + bc + " FROM bookings WHERE user_id=$1"

	args := []any{id}
	statusFilter := q.Get("status")
	if statusFilter == "" {
		sql += " AND status='active'"
	} else {
		if statusFilter != "active" && statusFilter != "canceled" {
			bad(w, "invalid status")
			return
		}
		args = append(args, statusFilter)
		sql += fmt.Sprintf(" AND status=$%d", len(args))
	}

	resourceID := q.Get("resource_id")
	if resourceID != "" {
		n, er := strconv.ParseInt(resourceID, 10, 64)
		if er != nil {
			bad(w, "invalid resource_id")
			return
		}
		args = append(args, n)
		sql += fmt.Sprintf(" AND resource_id=$%d", len(args))
	}

	from := q.Get("from")
	if from != "" {
		t, er := stamp(from)
		if er != nil {
			bad(w, "invalid timestamp")
			return
		}
		args = append(args, t)
		sql += fmt.Sprintf(" AND end_at>$%d", len(args))
	}

	to := q.Get("to")
	if to != "" {
		t, er := stamp(to)
		if er != nil {
			bad(w, "invalid timestamp")
			return
		}
		args = append(args, t)
		sql += fmt.Sprintf(" AND start_at<$%d", len(args))
	}

	rows, e := s.db.Query(r.Context(), sql+" ORDER BY start_at DESC", args...)
	if e != nil {
		dberr(w, e)
		return
	}

	defer rows.Close()

	var out []Booking
	for rows.Next() {
		v, e := sb(rows)
		if e != nil {
			dberr(w, e)
			return
		}
		out = append(out, v)
	}

	jsonOut(w, 200, out)
}
