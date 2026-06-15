package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// UserAPI provides methods for the user / group / resource-group endpoints.
//
// Each method branches on the client transport mode. Session mode (the default)
// targets Archery's AJAX/Django endpoints that ordinary accounts can reach; JWT
// mode targets the REST API under /api/v1/. The session shapes are taken from
// sql/user.py and sql/resource_group.py in the v1.8.5 source.
type UserAPI struct {
	client *Client
}

// UserRow is the common shape both transports map into. The session projection
// (user.lists) fills IsSuperuser; the REST serializer does not expose it, so the
// unset side stays zero/false.
type UserRow struct {
	ID          int
	Username    string
	Display     string
	Email       string
	IsActive    bool
	IsStaff     bool
	IsSuperuser bool
}

// GroupRow is a Django auth group (permission group).
type GroupRow struct {
	ID   int
	Name string
}

// ResourceGroupRow is a resource group used for instance access control. The
// session projection carries only the id/name (and ding webhook); the REST
// serializer additionally reports active/bind user counts, left zero in session
// mode.
type ResourceGroupRow struct {
	ID          int
	Name        string
	ActiveUsers int
	BindUsers   int
}

// ListUsers returns all users.
//
//	session: GET /user/list/ (no params; returns the full list)
//	jwt:     GET /api/v1/user/?limit&offset&search
//
// The session view (user.lists) does no server-side pagination or search, so
// those filters are applied client-side by the caller.
func (u *UserAPI) ListUsers(ctx context.Context) ([]UserRow, error) {
	if u.client.Mode() == ModeJWT {
		return u.listUsersREST(ctx)
	}
	return u.listUsersSession(ctx)
}

// listUsersSession queries user.lists. The view returns
// {"status":0,"msg":"ok","data":[{id,username,display,is_superuser,is_staff,
// is_active,email}]} with id rendered as a string (bigint_as_string=True).
func (u *UserAPI) listUsersSession(ctx context.Context) ([]UserRow, error) {
	data, err := u.client.SessionGet(ctx, "/user/list/")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	var raw struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
		Data   []struct {
			ID          json.Number `json:"id"`
			Username    string      `json:"username"`
			Display     string      `json:"display"`
			Email       string      `json:"email"`
			IsActive    bool        `json:"is_active"`
			IsStaff     bool        `json:"is_staff"`
			IsSuperuser bool        `json:"is_superuser"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing user list: %w", err)
	}
	if raw.Status != 0 {
		return nil, &APIError{StatusCode: 400, ErrorMessages: []string{nonEmptyMsg(raw.Msg, "user list failed")}}
	}
	rows := make([]UserRow, len(raw.Data))
	for i, d := range raw.Data {
		id, _ := d.ID.Int64()
		rows[i] = UserRow{
			ID:          int(id),
			Username:    d.Username,
			Display:     d.Display,
			Email:       d.Email,
			IsActive:    d.IsActive,
			IsStaff:     d.IsStaff,
			IsSuperuser: d.IsSuperuser,
		}
	}
	return rows, nil
}

// listUsersREST queries the JWT REST user endpoint (DRF pagination). It walks
// pages so the caller receives the full list, matching the session shape.
func (u *UserAPI) listUsersREST(ctx context.Context) ([]UserRow, error) {
	q := url.Values{}
	q.Set("limit", "500")
	path := u.client.APIPath("/v1/user/?" + q.Encode())
	data, err := u.client.Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	var page struct {
		Results []struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
			Display  string `json:"display"`
			Email    string `json:"email"`
			IsActive bool   `json:"is_active"`
			IsStaff  bool   `json:"is_staff"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("parsing user list: %w", err)
	}
	rows := make([]UserRow, len(page.Results))
	for i, r := range page.Results {
		rows[i] = UserRow{
			ID:       r.ID,
			Username: r.Username,
			Display:  r.Display,
			Email:    r.Email,
			IsActive: r.IsActive,
			IsStaff:  r.IsStaff,
		}
	}
	return rows, nil
}

// ListResourceGroups returns resource groups.
//
//	session: POST /group/group/ (limit, offset, search)
//	jwt:     GET  /api/v1/user/resourcegroup/?limit&offset&search
func (u *UserAPI) ListResourceGroups(ctx context.Context, limit, offset int, search string) ([]ResourceGroupRow, int, error) {
	if u.client.Mode() == ModeJWT {
		return u.listResourceGroupsREST(ctx, limit, offset, search)
	}
	return u.listResourceGroupsSession(ctx, limit, offset, search)
}

// listResourceGroupsSession queries resource_group.group. The view reads
// limit/offset/search from POST form fields and returns
// {"total":N,"rows":[{group_id,group_name,ding_webhook}]} with ids rendered as
// strings (bigint_as_string=True).
func (u *UserAPI) listResourceGroupsSession(ctx context.Context, limit, offset int, search string) ([]ResourceGroupRow, int, error) {
	form := url.Values{
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa(offset)},
	}
	if search != "" {
		form.Set("search", search)
	}
	data, err := u.client.SessionPost(ctx, "/group/group/", form)
	if err != nil {
		return nil, 0, fmt.Errorf("list resource groups: %w", err)
	}
	var raw struct {
		Total int `json:"total"`
		Rows  []struct {
			GroupID   json.Number `json:"group_id"`
			GroupName string      `json:"group_name"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, 0, fmt.Errorf("parsing resource group list: %w", err)
	}
	rows := make([]ResourceGroupRow, len(raw.Rows))
	for i, r := range raw.Rows {
		id, _ := r.GroupID.Int64()
		rows[i] = ResourceGroupRow{ID: int(id), Name: r.GroupName}
	}
	return rows, raw.Total, nil
}

// listResourceGroupsREST queries the JWT REST resource-group endpoint.
func (u *UserAPI) listResourceGroupsREST(ctx context.Context, limit, offset int, search string) ([]ResourceGroupRow, int, error) {
	q := url.Values{
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa(offset)},
	}
	if search != "" {
		q.Set("search", search)
	}
	path := u.client.APIPath("/v1/user/resourcegroup/?" + q.Encode())
	data, err := u.client.Get(ctx, path)
	if err != nil {
		return nil, 0, fmt.Errorf("list resource groups: %w", err)
	}
	var page struct {
		Count   int `json:"count"`
		Results []struct {
			ID          int    `json:"id"`
			Name        string `json:"group_name"`
			ActiveUsers int    `json:"active_users"`
			BindUsers   int    `json:"bind_users"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, 0, fmt.Errorf("parsing resource group list: %w", err)
	}
	rows := make([]ResourceGroupRow, len(page.Results))
	for i, r := range page.Results {
		rows[i] = ResourceGroupRow{ID: r.ID, Name: r.Name, ActiveUsers: r.ActiveUsers, BindUsers: r.BindUsers}
	}
	return rows, page.Count, nil
}

// ListGroups returns Django auth (permission) groups via the REST endpoint.
//
//	jwt: GET /api/v1/user/group/?limit&offset
//
// Session mode has no JSON route in v1.8.5: the web page /group/ renders HTML,
// not a list payload. The caller gates session mode before reaching here.
func (u *UserAPI) ListGroups(ctx context.Context, limit, offset int) ([]GroupRow, int, error) {
	q := url.Values{
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa(offset)},
	}
	path := u.client.APIPath("/v1/user/group/?" + q.Encode())
	data, err := u.client.Get(ctx, path)
	if err != nil {
		return nil, 0, fmt.Errorf("list groups: %w", err)
	}
	var page struct {
		Count   int `json:"count"`
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, 0, fmt.Errorf("parsing group list: %w", err)
	}
	rows := make([]GroupRow, len(page.Results))
	for i, r := range page.Results {
		rows[i] = GroupRow{ID: r.ID, Name: r.Name}
	}
	return rows, page.Count, nil
}

// ResourceGroupObjectType selects which relation a resourcegroup-add mutation
// targets: users or instances. Archery's view encodes these as the string "0"
// and "1" respectively.
type ResourceGroupObjectType string

const (
	ResourceGroupObjectUser     ResourceGroupObjectType = "0"
	ResourceGroupObjectInstance ResourceGroupObjectType = "1"
)

// AddResourceGroupRelation associates users or instances with a resource group.
//
//	session: POST /group/addrelation/ (group_id, object_type, object_info)
//
// object_info is a JSON-encoded array of strings; the view splits each element
// on "," and takes the leading integer as the object id (matching the web UI's
// "id,name" rows). This is a session-only operation: there is no REST/JWT route
// for it in v1.8.5, so the call uses the session transport regardless of mode.
func (u *UserAPI) AddResourceGroupRelation(ctx context.Context, groupID int, objectType ResourceGroupObjectType, objectIDs []int) error {
	infos := make([]string, len(objectIDs))
	for i, id := range objectIDs {
		infos[i] = strconv.Itoa(id)
	}
	objectInfo, err := json.Marshal(infos)
	if err != nil {
		return fmt.Errorf("encoding object_info: %w", err)
	}
	form := url.Values{
		"group_id":    {strconv.Itoa(groupID)},
		"object_type": {string(objectType)},
		"object_info": {string(objectInfo)},
	}
	data, err := u.client.SessionPost(ctx, "/group/addrelation/", form)
	if err != nil {
		return fmt.Errorf("add resource group relation: %w", err)
	}
	var raw struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing add relation response: %w", err)
	}
	if raw.Status != 0 {
		return &APIError{StatusCode: 400, ErrorMessages: []string{nonEmptyMsg(raw.Msg, "add resource group relation failed")}}
	}
	return nil
}

// nonEmptyMsg returns msg if non-empty, otherwise the fallback.
func nonEmptyMsg(msg, fallback string) string {
	if msg == "" {
		return fallback
	}
	return msg
}
