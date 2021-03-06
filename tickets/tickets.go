// Package tickets provides access to a project's tickets via the
// Lighthouse API.  http://help.lighthouseapp.com/kb/api/tickets.
package tickets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nwidger/lighthouse"
)

const (
	DefaultLimit = 30
	MaxLimit     = 100
)

type Service struct {
	basePath string
	s        *lighthouse.Service
}

func NewService(s *lighthouse.Service, projectID int) *Service {
	return &Service{
		basePath: s.BasePath + "/projects/" + strconv.Itoa(projectID) + "/tickets",
		s:        s,
	}
}

type Tag struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Tags []*Tag

type TagResponse struct {
	Tag *Tag `json:"tag"`
}

type TagsResponse struct {
	Tags []*TagResponse `json:"tags"`
}

type Attachment struct {
	AttachmentFileProcessing bool       `json:"attachment_file_processing"`
	Code                     string     `json:"code"`
	ContentType              string     `json:"content_type"`
	CreatedAt                *time.Time `json:"created_at"`
	Filename                 string     `json:"filename"`
	Height                   int        `json:"height"`
	ID                       int        `json:"id"`
	ProjectID                int        `json:"project_id"`
	Size                     int        `json:"size"`
	UploaderID               int        `json:"uploader_id"`
	Width                    int        `json:"width"`
	URL                      string     `json:"url"`
}

type Attachments []*Attachment

type AttachmentResponse struct {
	Attachment *Attachment `json:"attachment"`
}

type AttachmentsResponse struct {
	Attachments []*AttachmentResponse `json:"attachments"`
}

type AlphabeticalTag struct {
	Tag   string
	Count int
}

func (at *AlphabeticalTag) MarshalJSON() ([]byte, error) {
	tag, count := "", 0
	if at != nil {
		tag, count = at.Tag, at.Count
	}

	arr := []interface{}{tag, count}
	return json.Marshal(&arr)
}

func (at *AlphabeticalTag) UnmarshalJSON(data []byte) error {
	if data == nil {
		return nil
	}

	if at == nil {
		at = &AlphabeticalTag{}
	}

	at.Tag = ""
	at.Count = 0

	arr := []interface{}{}
	err := json.Unmarshal(data, &arr)
	if err != nil {
		return err
	}

	if len(arr) != 2 {
		return fmt.Errorf("AlphabeticalTag.UnmarshalJSON: length is %d, expected 2", len(arr))
	}

	tag, ok := arr[0].(string)
	if !ok {
		return fmt.Errorf("AlphabeticalTag.UnmarshalJSON: first element not a string")
	}
	at.Tag = tag

	count, ok := arr[1].(float64)
	if !ok {
		return fmt.Errorf("AlphabeticalTag.UnmarshalJSON: first element not an int")
	}
	at.Count = int(count)

	return nil
}

type AlphabeticalTags []*AlphabeticalTag

type DiffableAttributes struct {
	State        string `json:"state,omitempty"`
	Title        string `json:"title,omitempty"`
	AssignedUser int    `json:"assigned_user,omitempty"`
	Milestone    int    `json:"milestone,omitempty"`
	Tag          string `json:"tag,omitempty"`
}

type TicketVersion struct {
	AssignedUserID     int                 `json:"assigned_user_id"`
	AttachmentsCount   int                 `json:"attachments_count"`
	Body               string              `json:"body"`
	BodyHTML           string              `json:"body_html"`
	Closed             bool                `json:"closed"`
	CreatedAt          *time.Time          `json:"created_at"`
	CreatorID          int                 `json:"creator_id"`
	DiffableAttributes *DiffableAttributes `json:"diffable_attributes,omitempty"`
	Importance         int                 `json:"importance"`
	MilestoneID        int                 `json:"milestone_id"`
	MilestoneOrder     int                 `json:"milestone_order"`
	Number             int                 `json:"number"`
	Permalink          string              `json:"permalink"`
	ProjectID          int                 `json:"project_id"`
	RawData            []byte              `json:"raw_data"`
	Spam               bool                `json:"spam"`
	State              string              `json:"state,omitempty"`
	Tag                string              `json:"tag"`
	Title              string              `json:"title"`
	UpdatedAt          *time.Time          `json:"updated_at"`
	UserID             int                 `json:"user_id"`
	Version            int                 `json:"version"`
	WatchersIDs        []int               `json:"watchers_ids"`
	UserName           string              `json:"user_name"`
	CreatorName        string              `json:"creator_name"`
	URL                string              `json:"url"`
	Priority           int                 `json:"priority"`
	StateColor         string              `json:"state_color"`
}

type TicketVersions []*TicketVersion

type Ticket struct {
	AssignedUserID   int                   `json:"assigned_user_id"`
	AttachmentsCount int                   `json:"attachments_count"`
	Body             string                `json:"body"`
	BodyHTML         string                `json:"body_html"`
	Closed           bool                  `json:"closed"`
	CreatedAt        *time.Time            `json:"created_at"`
	CreatorID        int                   `json:"creator_id"`
	Importance       int                   `json:"importance"`
	MilestoneDueOn   *time.Time            `json:"milestone_due_on"`
	MilestoneID      int                   `json:"milestone_id"`
	MilestoneOrder   int                   `json:"milestone_order"`
	Number           int                   `json:"number"`
	Permalink        string                `json:"permalink"`
	ProjectID        int                   `json:"project_id"`
	RawData          []byte                `json:"raw_data"`
	Spam             bool                  `json:"spam"`
	State            string                `json:"state,omitempty"`
	Tag              string                `json:"tag"`
	Title            string                `json:"title"`
	UpdatedAt        *time.Time            `json:"updated_at"`
	UserID           int                   `json:"user_id"`
	Version          int                   `json:"version"`
	WatchersIDs      []int                 `json:"watchers_ids"`
	UserName         string                `json:"user_name"`
	CreatorName      string                `json:"creator_name"`
	AssignedUserName string                `json:"assigned_user_name"`
	URL              string                `json:"url"`
	MilestoneTitle   string                `json:"milestone_title"`
	Priority         int                   `json:"priority"`
	ImportanceName   string                `json:"importance_name"`
	OriginalBody     string                `json:"original_body"`
	LatestBody       string                `json:"latest_body"`
	OriginalBodyHTML string                `json:"original_body_html"`
	StateColor       string                `json:"state_color"`
	Tags             []*TagResponse        `json:"tags"`
	AlphabeticalTags AlphabeticalTags      `json:"alphabetical_tags"`
	Versions         TicketVersions        `json:"versions"`
	Attachments      []*AttachmentResponse `json:"attachments"`
}

type Tickets []*Ticket

type TicketCreate struct {
	Title          string `json:"title"`
	Body           string `json:"body"`
	State          string `json:"state,omitempty"`
	AssignedUserID int    `json:"assigned_user_id,omitempty"`
	MilestoneID    int    `json:"milestone_id,omitempty"`
	Tag            string `json:"tag"`

	// See:
	// http://help.lighthouseapp.com/discussions/api-developers/196-change-ticket-notifications
	NotifyAll        *bool `json:"notify_all,omitempty"`
	MultipleWatchers []int `json:"multiple_watchers,omitempty"`
}

type TicketUpdate struct {
	*Ticket

	NotifyAll        *bool `json:"notify_all,omitempty"`
	MultipleWatchers []int `json:"multiple_watchers,omitempty"`
}

type ticketRequest struct {
	Ticket interface{} `json:"ticket"`
}

func (mr *ticketRequest) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	return enc.Encode(mr)
}

type ticketResponse struct {
	Ticket *Ticket `json:"ticket"`
}

func (mr *ticketResponse) decode(r io.Reader) error {
	dec := json.NewDecoder(r)
	return dec.Decode(mr)
}

type ticketsResponse struct {
	Tickets []*ticketResponse `json:"tickets"`
}

func (msr *ticketsResponse) decode(r io.Reader) error {
	dec := json.NewDecoder(r)
	return dec.Decode(msr)
}

func (msr *ticketsResponse) tickets() Tickets {
	ms := make(Tickets, 0, len(msr.Tickets))
	for _, m := range msr.Tickets {
		ms = append(ms, m.Ticket)
	}

	return ms
}

type bulkEditRequest struct {
	Query          string `json:"query,omitempty"`
	Command        string `json:"command,omitempty"`
	MigrationToken string `json:"migration_token,omitempty"`
}

func (br *bulkEditRequest) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	return enc.Encode(br)
}

type ListOptions struct {
	// Search query, see
	// http://help.lighthouseapp.com/faqs/getting-started/how-do-i-search-for-tickets.
	// Default sort is by last update.
	Query string

	// If non-zero, the number of tickets per page to return.
	// Default is DefaultLimit, max is MaxLimit.
	Limit int

	// If non-zero, the page to return
	Page int
}

func (s *Service) List(opts *ListOptions) (Tickets, error) {
	path := s.basePath + ".json"
	if opts != nil {
		u, err := url.Parse(path)
		if err != nil {
			return nil, err
		}
		values := &url.Values{}
		if len(opts.Query) > 0 {
			values.Set("q", opts.Query)
		}
		if opts.Limit > 0 {
			values.Set("limit", strconv.Itoa(opts.Limit))
		}
		if opts.Page > 0 {
			values.Set("page", strconv.Itoa(opts.Page))
		}
		u.RawQuery = values.Encode()
		path = u.String()
	}

	resp, err := s.s.RoundTrip("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	err = lighthouse.CheckResponse(resp, http.StatusOK)
	if err != nil {
		return nil, err
	}

	tsresp := &ticketsResponse{}
	err = tsresp.decode(resp.Body)
	if err != nil {
		return nil, err
	}

	return tsresp.tickets(), nil
}

// ListAll repeatedly calls List and returns all pages.  ListAll
// ignores opts.Page.
func (s *Service) ListAll(opts *ListOptions) (Tickets, error) {
	realOpts := ListOptions{}
	if opts != nil {
		realOpts = *opts
	}

	ts := Tickets{}

	for realOpts.Page = 1; ; realOpts.Page++ {
		p, err := s.List(&realOpts)
		if err != nil {
			return nil, err
		}
		if len(p) == 0 {
			break
		}

		ts = append(ts, p...)
	}

	return ts, nil
}

// Only the fields in TicketUpdate can be set.
func (s *Service) Update(t *Ticket) error {
	treq := &ticketRequest{
		Ticket: &TicketUpdate{
			Ticket: t,
		},
	}

	buf := &bytes.Buffer{}
	err := treq.Encode(buf)
	if err != nil {
		return err
	}

	resp, err := s.s.RoundTrip("PUT", s.basePath+"/"+strconv.Itoa(t.Number)+".json", buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	err = lighthouse.CheckResponse(resp, http.StatusOK)
	if err != nil {
		return err
	}

	return nil
}

func (s *Service) New() (*Ticket, error) {
	return s.get("new")
}

// Get ticket using ticket number string, possibly prefixed by #
func (s *Service) Get(numberStr string) (*Ticket, error) {
	number, err := Number(numberStr)
	if err != nil {
		return nil, err
	}
	return s.GetByNumber(number)
}

func (s *Service) GetByNumber(number int) (*Ticket, error) {
	return s.get(strconv.Itoa(number))
}

func (s *Service) get(number string) (*Ticket, error) {
	resp, err := s.s.RoundTrip("GET", s.basePath+"/"+number+".json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	err = lighthouse.CheckResponse(resp, http.StatusOK)
	if err != nil {
		return nil, err
	}

	tresp := &ticketResponse{}
	err = tresp.decode(resp.Body)
	if err != nil {
		return nil, err
	}

	return tresp.Ticket, nil
}

// Only the fields in TicketCreate can be set.
func (s *Service) Create(t *Ticket) (*Ticket, error) {
	treq := &ticketRequest{
		Ticket: &TicketCreate{
			Title:          t.Title,
			Body:           t.Body,
			State:          t.State,
			AssignedUserID: t.AssignedUserID,
			MilestoneID:    t.MilestoneID,
			Tag:            t.Tag,
		},
	}

	buf := &bytes.Buffer{}
	err := treq.Encode(buf)
	if err != nil {
		return nil, err
	}

	resp, err := s.s.RoundTrip("POST", s.basePath+".json", buf)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	err = lighthouse.CheckResponse(resp, http.StatusCreated)
	if err != nil {
		return nil, err
	}

	tresp := &ticketResponse{
		Ticket: t,
	}
	err = tresp.decode(resp.Body)
	if err != nil {
		return nil, err
	}

	return t, nil
}

// Delete ticket using ticket number string, possibly prefixed by #
func (s *Service) Delete(numberStr string) error {
	number, err := Number(numberStr)
	if err != nil {
		return err
	}
	return s.DeleteByNumber(number)
}

func (s *Service) DeleteByNumber(number int) error {
	resp, err := s.s.RoundTrip("DELETE", s.basePath+"/"+strconv.Itoa(number)+".json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	err = lighthouse.CheckResponse(resp, http.StatusOK)
	if err != nil {
		return err
	}

	return nil
}

func (s *Service) GetAttachment(a *Attachment) (io.ReadCloser, error) {
	resp, err := s.s.RoundTrip("GET", a.URL, nil)
	if err != nil {
		return nil, err
	}

	err = lighthouse.CheckResponse(resp, http.StatusOK)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

func (s *Service) AddAttachment(t *Ticket, filename string, r io.Reader) error {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	attachmentPart, err := w.CreateFormFile("ticket[attachment][]", filepath.Base(filename))
	if err != nil {
		return err
	}

	_, err = io.Copy(attachmentPart, r)
	if err != nil {
		return err
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="json"`)
	h.Set("Content-Type", "application/json")

	ticketPart, err := w.CreatePart(h)
	if err != nil {
		return err
	}

	treq := &ticketRequest{
		Ticket: &TicketUpdate{
			Ticket: t,
		},
	}

	err = treq.Encode(ticketPart)
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", s.basePath+"/"+strconv.Itoa(t.Number)+".json", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := s.s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	err = lighthouse.CheckResponse(resp, http.StatusOK)
	if err != nil {
		return err
	}

	return nil
}

type BulkEditOptions struct {
	// This is any search query that is valid on the tickets
	// page. You can also reference all tickets with 'all' or a
	// single ticket with just the ticket's number.  See
	// http://help.lighthouseapp.com/faqs/getting-started/how-do-i-search-for-tickets.
	Query string

	// This is a string of commands using the keywords from Query.
	Command string

	// MigrationToken is the API token of a user that has access
	// to the project that a ticket is migrating to.  If the
	// 'project' or 'account' keywords are not used, this is
	// ignored.  Otherwise, MigrationToken must be set.
	MigrationToken string
}

// Undocumented, see
// https://lighthouse.tenderapp.com/kb/ticket-workflow/how-do-i-update-tickets-with-keywords
// and http://pastie.org/460585.
func (s *Service) BulkEdit(opts *BulkEditOptions) error {
	breq := &bulkEditRequest{
		Query:          opts.Query,
		Command:        opts.Command,
		MigrationToken: opts.MigrationToken,
	}

	buf := &bytes.Buffer{}
	err := breq.Encode(buf)
	if err != nil {
		return err
	}

	resp, err := s.s.RoundTrip("POST", strings.TrimSuffix(s.basePath, "/tickets")+"/bulk_edit.json", buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	err = lighthouse.CheckResponse(resp, http.StatusOK)
	if err != nil {
		return err
	}

	return nil
}

// Return ticket number from string, possibly prefixed with #
func Number(numberStr string) (int, error) {
	str := numberStr
	if strings.HasPrefix(str, "#") {
		str = str[1:]
	}
	number, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid ticket number %q", numberStr)
	}
	return int(number), nil
}
