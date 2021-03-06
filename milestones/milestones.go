// Package milestones provides access to a project's milestones via
// the Lighthouse API.
// http://help.lighthouseapp.com/kb/api/milestones.
package milestones

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nwidger/lighthouse"
)

type Service struct {
	basePath string
	s        *lighthouse.Service
}

func NewService(s *lighthouse.Service, projectID int) *Service {
	return &Service{
		basePath: s.BasePath + "/projects/" + strconv.Itoa(projectID) + "/milestones",
		s:        s,
	}
}

type Milestone struct {
	AttachmentsCount int        `json:"attachments_count"`
	CompletedAt      *time.Time `json:"completed_at"`
	CreatedAt        *time.Time `json:"created_at"`
	DueOn            *time.Time `json:"due_on"`
	Goals            string     `json:"goals"`
	GoalsHTML        string     `json:"goals_html"`
	ID               int        `json:"id"`
	MaxPoints        int        `json:"max_points"`
	OpenTicketsCount int        `json:"open_tickets_count"`
	Permalink        string     `json:"permalink"`
	PointsClosed     int        `json:"points_closed"`
	PointsOpen       int        `json:"points_open"`
	Position         int        `json:"position"`
	ProjectID        int        `json:"project_id"`
	TicketsCount     int        `json:"tickets_count"`
	Title            string     `json:"title"`
	UpdatedAt        *time.Time `json:"updated_at"`
	URL              string     `json:"url"`
	UserName         string     `json:"user_name"`
}

type Milestones []*Milestone

type MilestoneCreate struct {
	Goals string     `json:"goals"`
	Title string     `json:"title"`
	DueOn *time.Time `json:"due_on"`
}

type MilestoneUpdate struct {
	Goals string     `json:"goals"`
	Title string     `json:"title"`
	DueOn *time.Time `json:"due_on"`
}

type milestoneRequest struct {
	Milestone interface{} `json:"milestone"`
}

func (mr *milestoneRequest) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	return enc.Encode(mr)
}

type milestoneResponse struct {
	Milestone *Milestone `json:"milestone"`
}

func (mr *milestoneResponse) decode(r io.Reader) error {
	dec := json.NewDecoder(r)
	return dec.Decode(mr)
}

type milestonesResponse struct {
	Milestones []*milestoneResponse `json:"milestones"`
}

func (msr *milestonesResponse) decode(r io.Reader) error {
	dec := json.NewDecoder(r)
	return dec.Decode(msr)
}

func (msr *milestonesResponse) milestones() Milestones {
	ms := make(Milestones, 0, len(msr.Milestones))
	for _, m := range msr.Milestones {
		ms = append(ms, m.Milestone)
	}

	return ms
}

type ListOptions struct {
	// If non-zero, the page to return
	Page int
}

// ListAll repeatedly calls List and returns all pages.  ListAll
// ignores opts.Page.
func (s *Service) ListAll(opts *ListOptions) (Milestones, error) {
	realOpts := ListOptions{}
	if opts != nil {
		realOpts = *opts
	}

	ms := Milestones{}

	for realOpts.Page = 1; ; realOpts.Page++ {
		p, err := s.List(&realOpts)
		if err != nil {
			return nil, err
		}
		if len(p) == 0 {
			break
		}

		ms = append(ms, p...)
	}

	return ms, nil
}

func (s *Service) List(opts *ListOptions) (Milestones, error) {
	path := s.basePath + ".json"
	if opts != nil {
		u, err := url.Parse(path)
		if err != nil {
			return nil, err
		}
		values := &url.Values{}
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

	msresp := &milestonesResponse{}
	err = msresp.decode(resp.Body)
	if err != nil {
		return nil, err
	}

	return msresp.milestones(), nil
}

func (s *Service) New() (*Milestone, error) {
	return s.get("new")
}

// Only the fields in MilestoneUpdate can be set.
func (s *Service) Update(m *Milestone) error {
	mreq := &milestoneRequest{
		Milestone: &MilestoneUpdate{
			Goals: m.Goals,
			Title: m.Title,
			DueOn: m.DueOn,
		},
	}

	buf := &bytes.Buffer{}
	err := mreq.Encode(buf)
	if err != nil {
		return err
	}

	resp, err := s.s.RoundTrip("PUT", s.basePath+"/"+strconv.Itoa(m.ID)+".json", buf)
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

func (s *Service) Get(idOrTitle string) (*Milestone, error) {
	id, err := lighthouse.ID(idOrTitle)
	if err == nil {
		return s.GetByID(id)
	}
	return s.GetByTitle(idOrTitle)
}

func (s *Service) GetByID(id int) (*Milestone, error) {
	return s.get(strconv.Itoa(id))
}

func (s *Service) GetByTitle(title string) (*Milestone, error) {
	ms, err := s.ListAll(&ListOptions{})
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(title)
	for _, m := range ms {
		if strings.ToLower(m.Title) == lower {
			return m, nil
		}
	}
	return nil, fmt.Errorf("no such milestone %q", title)
}

func (s *Service) get(id string) (*Milestone, error) {
	resp, err := s.s.RoundTrip("GET", s.basePath+"/"+id+".json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	err = lighthouse.CheckResponse(resp, http.StatusOK)
	if err != nil {
		return nil, err
	}

	mresp := &milestoneResponse{}
	err = mresp.decode(resp.Body)
	if err != nil {
		return nil, err
	}

	return mresp.Milestone, nil
}

// Only the fields in MilestoneCreate can be set.
func (s *Service) Create(m *Milestone) (*Milestone, error) {
	mreq := &milestoneRequest{
		Milestone: &MilestoneCreate{
			Goals: m.Goals,
			Title: m.Title,
			DueOn: m.DueOn,
		},
	}

	buf := &bytes.Buffer{}
	err := mreq.Encode(buf)
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

	mresp := &milestoneResponse{
		Milestone: m,
	}
	err = mresp.decode(resp.Body)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (s *Service) Close(idOrTitle string) error {
	id, err := lighthouse.ID(idOrTitle)
	if err == nil {
		return s.CloseByID(id)
	}
	return s.CloseByTitle(idOrTitle)
}

func (s *Service) CloseByID(id int) error {
	resp, err := s.s.RoundTrip("PUT", s.basePath+"/"+strconv.Itoa(id)+"/close.json", nil)
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

func (s *Service) CloseByTitle(title string) error {
	m, err := s.GetByTitle(title)
	if err != nil {
		return err
	}
	return s.CloseByID(m.ID)
}

func (s *Service) Open(idOrTitle string) error {
	id, err := lighthouse.ID(idOrTitle)
	if err == nil {
		return s.OpenByID(id)
	}
	return s.OpenByTitle(idOrTitle)
}

func (s *Service) OpenByID(id int) error {
	resp, err := s.s.RoundTrip("PUT", s.basePath+"/"+strconv.Itoa(id)+"/open.json", nil)
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

func (s *Service) OpenByTitle(title string) error {
	m, err := s.GetByTitle(title)
	if err != nil {
		return err
	}
	return s.OpenByID(m.ID)
}

func (s *Service) Delete(idOrTitle string) error {
	id, err := lighthouse.ID(idOrTitle)
	if err == nil {
		return s.DeleteByID(id)
	}
	return s.DeleteByTitle(idOrTitle)
}

func (s *Service) DeleteByID(id int) error {
	resp, err := s.s.RoundTrip("DELETE", s.basePath+"/"+strconv.Itoa(id)+".json", nil)
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

func (s *Service) DeleteByTitle(title string) error {
	m, err := s.GetByTitle(title)
	if err != nil {
		return err
	}
	return s.DeleteByID(m.ID)
}
