package main

import (
	"bytes"
	"crypto/tls"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mholt/archiver"
	"github.com/nwidger/lighthouse"
	"github.com/nwidger/lighthouse/milestones"
	"github.com/nwidger/lighthouse/profiles"
	"github.com/nwidger/lighthouse/projects"
	"github.com/nwidger/lighthouse/tickets"
	"github.com/nwidger/lighthouse/users"
	gitlab "github.com/xanzy/go-gitlab"
)

var (
	usersMap     = map[int]*gitlab.User{}
	usersNameMap = map[string]*gitlab.User{}

	projectsMap   = map[int]*gitlab.Project{}
	milestonesMap = map[int]*gitlab.Milestone{}
	issuesMap     = map[int]*gitlab.Issue{}

	groupsMap = map[string]*gitlab.Group{}
)

func main() {
	export := ""
	token := ""
	baseURL := ""
	usersPath := ""
	groupsPath := ""
	password := "changeme"
	project := ""
	milestone := ""
	number := 0
	delete := false
	stateKey := "lh"
	insecure := false

	flag.StringVar(&token, "token", token, "GitLab API token to use")
	flag.StringVar(&baseURL, "base-url", baseURL, "GitLab base URL to use (i.e., https://gitlab.example.com/)")
	flag.StringVar(&usersPath, "users", usersPath, "Path to JSON file mapping Lighthouse user ID's to GitLab users")
	flag.StringVar(&groupsPath, "groups", groupsPath, "Path to JSON file containing GitLab groups to create")
	flag.StringVar(&password, "password", password, "Password to use when creating GitLab users")
	flag.StringVar(&project, "project", project, "Only migrate projects with the given name (useful for testing)")
	flag.StringVar(&milestone, "milestone", milestone, "Only migrate milestones with the given title (useful for testing)")
	flag.StringVar(&stateKey, "state-key", stateKey, "Scoped label key used to map Lighthouse ticket states to GitLab scoped labels")
	flag.IntVar(&number, "number", number, "Only migrate tickets with the given number (useful for testing)")
	flag.BoolVar(&delete, "delete", delete, "Do not import, delete all GitLab projects, groups and users (except root user and user owning API token -token) and then exit")
	flag.BoolVar(&insecure, "insecure", insecure, "Allow insecure HTTPS connections to GitLab API")

	flag.Parse()

	if len(flag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "Must specify path to Lighthouse export file\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if len(baseURL) == 0 {
		fmt.Fprintf(os.Stderr, "Must specify GitLab base URL via -base-url\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if len(usersPath) == 0 {
		fmt.Fprintf(os.Stderr, "Must specify path to Lighthouse users map file via -users\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if len(token) == 0 {
		fmt.Fprintf(os.Stderr, "Must specify GitLab API token via -token\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if len(password) == 0 {
		fmt.Fprintf(os.Stderr, "Must specify password for creating GitLab users via -password\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if len(stateKey) == 0 {
		fmt.Fprintf(os.Stderr, "Must specify scoped label key for mapping Lighthouse ticket states to GitLab scoped labels via -state-key\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	export = flag.Arg(0)

	exp, tempDir, err := readLHExport(export)
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	defer signal.Reset(os.Interrupt)

	go func(c chan os.Signal) {
		<-c
		signal.Reset(os.Interrupt)
		if len(tempDir) > 0 {
			os.RemoveAll(tempDir)
		}
		os.Exit(1)
	}(c)

	var client *http.Client
	if insecure {
		client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}

	git := gitlab.NewClient(client, token)
	err = git.SetBaseURL(baseURL)
	if err != nil {
		log.Fatal(err)
	}

	me, _, err := git.Users.CurrentUser()
	if err != nil {
		log.Fatal(err)
	}

	if delete {
		gs, _, err := git.Groups.ListGroups(&gitlab.ListGroupsOptions{})
		if err != nil {
			log.Fatal(err)
		}
		for _, g := range gs {
			fmt.Println("deleting group", g.Name)
			_, err = git.Groups.DeleteGroup(g.ID)
			if err != nil {
				log.Fatal(err)
			}
		}
		ps, _, err := git.Projects.ListProjects(&gitlab.ListProjectsOptions{})
		if err != nil {
			log.Fatal(err)
		}
		for _, p := range ps {
			fmt.Println("deleting project", p.Name)
			_, err = git.Projects.DeleteProject(p.ID)
			if err != nil {
				log.Fatal(err)
			}
		}
		us, _, err := git.Users.ListUsers(&gitlab.ListUsersOptions{})
		if err != nil {
			log.Fatal(err)
		}
		for _, u := range us {
			if u.Username == "root" || u.Username == me.Username {
				continue
			}
			fmt.Println("deleting user", u.Username)
			git.Users.DeleteUser(u.ID)
			if err != nil {
				log.Fatal(err)
			}
		}

		return
	}

	f, err := os.Open(usersPath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	err = dec.Decode(&usersMap)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}

	for _, lhUser := range exp.users.list {
		userOpt, options, ok := lhUserToCreateUser(lhUser, password)
		if !ok {
			continue
		}
		fmt.Println("creating user", *userOpt.Username)
		u, _, err := git.Users.CreateUser(userOpt, options...)
		if err != nil {
			fmt.Fprintln(os.Stderr, "unable to create user", lhUser.Name, err)
			continue
		}
		usersMap[lhUser.ID] = u
		usersNameMap[lhUser.Name] = u
	}

	us, _, err := git.Users.ListUsers(&gitlab.ListUsersOptions{})
	for _, u := range us {
		for _, lhUser := range exp.users.list {
			if u.Name == lhUser.Name {
				usersMap[lhUser.ID] = u
				usersNameMap[lhUser.Name] = u
				break
			}
		}
	}

	var groups []struct {
		*gitlab.Group
		Projects []string `json:"projects"`
		Members  []string `json:"members"`
	}

	if len(groupsPath) > 0 {
		f, err = os.Open(groupsPath)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()

		dec = json.NewDecoder(f)
		err = dec.Decode(&groups)
		f.Close()
		if err != nil {
			log.Fatal(err)
		}
	}

	for _, group := range groups {
		fmt.Println("creating group", group.Name)
		g, _, err := git.Groups.CreateGroup(&gitlab.CreateGroupOptions{
			Name:        gitlab.String(group.Name),
			Path:        gitlab.String(group.Path),
			Description: gitlab.String(group.Description),
			Visibility:  gitlab.Visibility(gitlab.PrivateVisibility),
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "unable to create group", group.Name, err)
			continue
		}
		for _, lhProjectName := range group.Projects {
			groupsMap[sanitizeProjectName(lhProjectName)] = g
		}
		for _, member := range group.Members {
			u, ok := userByUsername(member)
			if !ok {
				continue
			}
			_, _, err = git.GroupMembers.AddGroupMember(g.ID, &gitlab.AddGroupMemberOptions{
				UserID:      gitlab.Int(u.ID),
				AccessLevel: gitlab.AccessLevel(gitlab.MaintainerPermissions),
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, "unable to add", member, "to group", group.Name, err)
			}
		}
	}

	for _, lhProject := range exp.projects.list {
		if len(project) > 0 && !strings.EqualFold(lhProject.Name, project) {
			continue
		}
		projectOpt, options, ok := lhProjectToCreateProject(lhProject)
		if !ok {
			continue
		}
		fmt.Println("creating project", *projectOpt.Name)
		p, _, err := git.Projects.CreateProject(projectOpt, options...)
		if err != nil {
			fmt.Fprintln(os.Stderr, "unable to create project", lhProject.Name, err)
			continue
		}
		projectsMap[lhProject.ID] = p

		labelOpts, options, ok := lhProjectToCreateLabels(lhProject, stateKey)
		if ok {
			for _, labelOpt := range labelOpts {
				_, _, err = git.Labels.CreateLabel(p.ID, labelOpt, options...)
				if err != nil {
					fmt.Fprintln(os.Stderr, "unable to create label", labelOpt.Name, "in project", lhProject.Name, err)
					continue
				}
			}
		}

		for _, lhMembership := range lhProject.memberships {
			memberOpt, options, ok := lhMembershipToAddProjectMember(lhMembership)
			if !ok {
				continue
			}
			_, _, err = git.ProjectMembers.AddProjectMember(p.ID, memberOpt, options...)
			if err != nil {
				fmt.Fprintln(os.Stderr, "unable to add", lhMembership.User.Name, "to project", lhProject.Name, err)
			}
		}

		for _, lhMilestone := range lhProject.milestones.list {
			if len(milestone) > 0 && !strings.EqualFold(lhMilestone.Title, milestone) {
				continue
			}
			createMilestoneOpt, options, ok := lhMilestoneToCreateMilestone(lhMilestone)
			if !ok {
				continue
			}
			fmt.Println("creating milestone", *createMilestoneOpt.Title)
			m, _, err := git.Milestones.CreateMilestone(p.ID, createMilestoneOpt, options...)
			if err != nil {
				fmt.Fprintln(os.Stderr, "unable to create milestone", lhMilestone.Title, "in project", lhProject.Name, err)
				continue
			}
			milestonesMap[lhMilestone.ID] = m

			updateMilestoneOpt, options, ok := lhMilestoneToUpdateMilestone(lhMilestone)
			if ok {
				_, _, err = git.Milestones.UpdateMilestone(p.ID, m.ID, updateMilestoneOpt, options...)
				if err != nil {
					fmt.Fprintln(os.Stderr, "unable to update milestone", lhMilestone.Title, "in project", lhProject.Name, err)
				}
			}
		}

		for _, lhTicket := range lhProject.tickets.list {
			if number > 0 && lhTicket.Number != number {
				continue
			}
			issueOpt, options, ok := lhTicketToCreateIssue(lhTicket, stateKey)
			if !ok {
				continue
			}
			fmt.Println("creating issue", *issueOpt.IID)
			i, _, err := git.Issues.CreateIssue(p.ID, issueOpt, options...)
			if err != nil {
				fmt.Fprintln(os.Stderr, "unable to create issue", lhTicket.Number, "in project", lhProject.Name, err)
				continue
			}
			issuesMap[lhTicket.Number] = i

			for _, watcherID := range lhTicket.WatchersIDs {
				options := withSudoByUserID(watcherID)
				_, _, err = git.Issues.SubscribeToIssue(p.ID, i.IID, options...)
				if err != nil && err != io.EOF {
					fmt.Fprintln(os.Stderr, "unable to subscribe user", watcherID, "to issue", i.IID, "in project", lhProject.Name, err)
				}
			}

			for _, lhVersion := range lhTicket.Versions {
				issueOpt, options, ok := lhTicketVersionToUpdateIssue(lhVersion, stateKey)
				if ok {
					_, _, err = git.Issues.UpdateIssue(p.ID, i.IID, issueOpt, options...)
					if err != nil {
						fmt.Fprintln(os.Stderr, "unable to update issue", i.IID, "in project", lhProject.Name, err)
					}
				}
				var pfs []*gitlab.ProjectFile
				for _, lhAttachment := range lhTicket.attachments.list {
					if lhAttachment.CreatedAt == nil || lhVersion.CreatedAt == nil ||
						!lhAttachment.CreatedAt.Equal(*lhVersion.CreatedAt) {
						continue
					}
					file, options, ok := lhAttachmentToUploadFile(lhAttachment)
					if !ok {
						continue
					}
					pf, _, err := git.Projects.UploadFile(p.ID, file, options...)
					if err != nil {
						fmt.Fprintln(os.Stderr, "unable to upload file", file, "for issue", i.IID, "in project", lhProject.Name, err)
						continue
					}
					pfs = append(pfs, pf)
				}
				noteOpt, options, ok := lhTicketVersionToCreateIssueNote(lhVersion, lhVersion.CreatedAt.Equal(*lhTicket.CreatedAt), pfs)
				if ok {
					_, _, err = git.Notes.CreateIssueNote(p.ID, i.IID, noteOpt, options...)
					if err != nil {
						fmt.Fprintln(os.Stderr, "unable to create issue note for issue", i.IID, "in project", lhProject.Name, err)
					}
				}
			}
		}
	}
}

func sanitizeProjectName(name string) string {
	return strings.ReplaceAll(name, `'`, ``)
}

func projectByID(id int) (*gitlab.Project, bool) {
	if id == 0 {
		return nil, false
	}
	p, ok := projectsMap[id]
	if !ok || p == nil {
		return nil, false
	}
	return p, true
}

func milestoneByID(id int) (*gitlab.Milestone, bool) {
	if id == 0 {
		return nil, false
	}
	m, ok := milestonesMap[id]
	if !ok || m == nil {
		return nil, false
	}
	return m, true
}

func issueByNumber(number int) (*gitlab.Issue, bool) {
	if number == 0 {
		return nil, false
	}
	i, ok := issuesMap[number]
	if !ok || i == nil {
		return nil, false
	}
	return i, true
}

func userByID(id int) (*gitlab.User, bool) {
	if id == 0 {
		return nil, false
	}
	u, ok := usersMap[id]
	if !ok || u == nil {
		return nil, false
	}
	return u, true
}

func userByUsername(username string) (*gitlab.User, bool) {
	if len(username) == 0 {
		return nil, false
	}
	u, ok := usersNameMap[username]
	if !ok || u == nil {
		return nil, false
	}
	return u, true
}

func withSudoByUserID(id int) []gitlab.OptionFunc {
	var options []gitlab.OptionFunc
	u, ok := userByID(id)
	if ok {
		options = append(options, gitlab.WithSudo(u.ID))
	}
	return options
}

func withSudoByUsername(username string) []gitlab.OptionFunc {
	var options []gitlab.OptionFunc
	u, ok := userByUsername(username)
	if ok {
		options = append(options, gitlab.WithSudo(u.ID))
	}
	return options
}

func lhUserToCreateUser(lhUser *lhUser, password string) (*gitlab.CreateUserOptions, []gitlab.OptionFunc, bool) {
	var options []gitlab.OptionFunc
	u, ok := userByID(lhUser.ID)
	if !ok {
		return nil, nil, false
	}
	opt := &gitlab.CreateUserOptions{
		Email:            gitlab.String(u.Email),
		Password:         gitlab.String(password),
		Username:         gitlab.String(u.Username),
		Name:             gitlab.String(u.Name),
		ProjectsLimit:    gitlab.Int(u.ProjectsLimit),
		Admin:            gitlab.Bool(u.IsAdmin),
		CanCreateGroup:   gitlab.Bool(u.CanCreateGroup),
		SkipConfirmation: gitlab.Bool(true),
		External:         gitlab.Bool(u.External),
	}
	return opt, options, true
}

func lhProjectToCreateProject(lhProject *lhProject) (*gitlab.CreateProjectOptions, []gitlab.OptionFunc, bool) {
	var options []gitlab.OptionFunc
	var name string
	name = sanitizeProjectName(lhProject.Name)
	var namespaceID *int
	g, ok := groupsMap[name]
	if ok {
		namespaceID = gitlab.Int(g.ID)
	}
	opt := &gitlab.CreateProjectOptions{
		Name:        gitlab.String(name),
		NamespaceID: namespaceID,
		Description: gitlab.String(lhtoGitLabMarkdown(lhProject.Description)),
		Visibility:  gitlab.Visibility(gitlab.PrivateVisibility),
	}
	return opt, options, true
}

func lhProjectToCreateLabels(lhProject *lhProject, stateKey string) ([]*gitlab.CreateLabelOptions, []gitlab.OptionFunc, bool) {
	var opts []*gitlab.CreateLabelOptions
	var options []gitlab.OptionFunc
	openLabels, ok := lhProjectStatesToCreateLabels(lhProject.OpenStates, stateKey)
	if !ok {
		return nil, nil, false
	}
	opts = append(opts, openLabels...)
	closedLabels, ok := lhProjectStatesToCreateLabels(lhProject.ClosedStates, stateKey)
	if !ok {
		return nil, nil, false
	}
	opts = append(opts, closedLabels...)
	return opts, options, true
}

var (
	lhStateDefinitionRegexp = regexp.MustCompile(`^\s*(?P<name>[^/]+)/(?P<color>[0-9a-fA-F]+)\s*(#\s*(?P<description>.*)\s*)?$`)
)

func lhProjectStatesToCreateLabels(text, stateKey string) ([]*gitlab.CreateLabelOptions, bool) {
	var opts []*gitlab.CreateLabelOptions
	for _, line := range strings.Split(text, "\n") {
		var name, color, description string

		names := lhStateDefinitionRegexp.SubexpNames()
		m := lhStateDefinitionRegexp.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		for i := range m {
			switch names[i] {
			case "name":
				name = stateKey + strings.TrimSpace(m[i])
			case "color":
				c := m[i]
				if len(c) == 3 {
					// convert 3-char color to 6-char color
					first, second, third := string(c[0]), string(c[1]), string(c[2])
					c = first + first + second + second + third + third
				}
				if len(c) == 6 {
					color = "#" + c
				}
			case "description":
				d := strings.TrimSpace(m[i])
				// ignore the default "help" descriptions
				if len(d) > 0 &&
					d != "You can add comments here" &&
					d != "if you want to." &&
					d != "You can customize colors" &&
					d != "with 3 or 6 character hex codes" &&
					d != "'A30' expands to 'AA3300'" {
					description = d
				}
			}
		}
		// color is mandatory, so pick a default
		if len(color) == 0 {
			color = "#428BCA"
		}
		opt := &gitlab.CreateLabelOptions{
			Name:        gitlab.String(name),
			Color:       gitlab.String(color),
			Description: gitlab.String(description),
		}
		opts = append(opts, opt)
	}
	return opts, true
}

func lhMembershipToAddProjectMember(lhMembership *projects.Membership) (*gitlab.AddProjectMemberOptions, []gitlab.OptionFunc, bool) {
	var options []gitlab.OptionFunc
	u, ok := userByID(lhMembership.UserID)
	if !ok {
		return nil, nil, false
	}
	opt := &gitlab.AddProjectMemberOptions{
		UserID:      gitlab.Int(u.ID),
		AccessLevel: gitlab.AccessLevel(gitlab.MaintainerPermissions),
	}
	return opt, options, true
}

func lhMilestoneToCreateMilestone(lhMilestone *milestones.Milestone) (*gitlab.CreateMilestoneOptions, []gitlab.OptionFunc, bool) {
	options := withSudoByUsername(lhMilestone.UserName)
	var startDate, dueDate *gitlab.ISOTime
	if lhMilestone.CreatedAt != nil {
		d := gitlab.ISOTime(*lhMilestone.CreatedAt)
		startDate = &d
	}
	if lhMilestone.DueOn != nil &&
		(lhMilestone.CreatedAt == nil || lhMilestone.DueOn.After(*lhMilestone.CreatedAt)) {
		d := gitlab.ISOTime(*lhMilestone.DueOn)
		dueDate = &d
	}
	opt := &gitlab.CreateMilestoneOptions{
		Title:       gitlab.String(lhMilestone.Title),
		Description: gitlab.String(lhtoGitLabMarkdown(lhMilestone.Goals)),
		StartDate:   startDate,
		DueDate:     dueDate,
	}
	return opt, options, true
}

func lhMilestoneToUpdateMilestone(lhMilestone *milestones.Milestone) (*gitlab.UpdateMilestoneOptions, []gitlab.OptionFunc, bool) {
	options := withSudoByUsername(lhMilestone.UserName)
	var stateEvent *string
	if lhMilestone.CompletedAt != nil {
		stateEvent = gitlab.String("close")
	} else {
		stateEvent = gitlab.String("activate")
	}
	if stateEvent == nil {
		return nil, nil, false
	}
	opt := &gitlab.UpdateMilestoneOptions{
		StateEvent: stateEvent,
	}
	return opt, options, true
}

func lhTicketToCreateIssue(lhTicket *lhTicket, stateKey string) (*gitlab.CreateIssueOptions, []gitlab.OptionFunc, bool) {
	options := withSudoByUserID(lhTicket.CreatorID)

	var title *string
	title = gitlab.String(lhTicket.Title)
	var description *string
	description = gitlab.String(lhtoGitLabMarkdown(lhTicket.Body))
	var assigneeIDs []int
	if lhTicket.AssignedUserID == 0 {
		assigneeIDs = append(assigneeIDs, 0)
	} else {
		u, ok := userByID(lhTicket.AssignedUserID)
		if ok {
			assigneeIDs = append(assigneeIDs, u.ID)
		}
	}
	var milestoneID *int
	if lhTicket.MilestoneID == 0 {
		milestoneID = gitlab.Int(0)
	} else {
		m, ok := milestoneByID(lhTicket.MilestoneID)
		if ok {
			milestoneID = gitlab.Int(m.ID)
		}
	}
	var labels gitlab.Labels
	labels = lhTicketToLabels(lhTicket, stateKey)
	var createdAt *time.Time
	if lhTicket.CreatedAt != nil {
		createdAt = lhTicket.CreatedAt
	}

	if len(lhTicket.Versions) > 0 {
		lhVersion := lhTicket.Versions[0]
		updateOpt, _, ok := lhTicketVersionToUpdateIssue(lhVersion, stateKey)
		if ok {
			assigneeIDs = updateOpt.AssigneeIDs
			milestoneID = updateOpt.MilestoneID
			labels = updateOpt.Labels
		}
	}

	opt := &gitlab.CreateIssueOptions{
		IID:         gitlab.Int(lhTicket.Number),
		Title:       title,
		Description: description,
		AssigneeIDs: assigneeIDs,
		MilestoneID: milestoneID,
		Labels:      labels,
		CreatedAt:   createdAt,
	}
	return opt, options, true
}

func lhTicketVersionToUpdateIssue(lhVersion *tickets.TicketVersion, stateKey string) (*gitlab.UpdateIssueOptions, []gitlab.OptionFunc, bool) {
	options := withSudoByUserID(lhVersion.UserID)
	var title *string
	title = gitlab.String(lhVersion.Title)
	var assigneeIDs []int
	if lhVersion.AssignedUserID == 0 {
		assigneeIDs = append(assigneeIDs, 0)
	} else {
		u, ok := userByID(lhVersion.AssignedUserID)
		if ok {
			assigneeIDs = append(assigneeIDs, u.ID)
		}
	}
	var milestoneID *int
	if lhVersion.MilestoneID == 0 {
		milestoneID = gitlab.Int(0)
	} else {
		m, ok := milestoneByID(lhVersion.MilestoneID)
		if ok {
			milestoneID = gitlab.Int(m.ID)
		}
	}
	labels := lhTicketVersionToLabels(lhVersion, stateKey)
	var stateEvent *string
	if lhVersion.Closed {
		stateEvent = gitlab.String("close")
	} else {
		stateEvent = gitlab.String("reopen")
	}
	var updatedAt *time.Time
	if lhVersion.UpdatedAt != nil {
		updatedAt = lhVersion.UpdatedAt
	}
	opt := &gitlab.UpdateIssueOptions{
		Title:       title,
		AssigneeIDs: assigneeIDs,
		MilestoneID: milestoneID,
		Labels:      labels,
		StateEvent:  stateEvent,
		UpdatedAt:   updatedAt,
	}
	return opt, options, true
}

func lhTicketVersionToCreateIssueNote(lhVersion *tickets.TicketVersion, currentVersion bool, pfs []*gitlab.ProjectFile) (*gitlab.CreateIssueNoteOptions, []gitlab.OptionFunc, bool) {
	options := withSudoByUserID(lhVersion.UserID)
	var createdAt *time.Time
	if lhVersion.CreatedAt != nil {
		createdAt = lhVersion.CreatedAt
	}
	var body string
	if !currentVersion {
		if len(body) > 0 {
			body += "\n\n"
		}
		body += lhtoGitLabMarkdown(lhVersion.Body)
	}
	for _, pf := range pfs {
		if len(body) > 0 {
			body += "\n\n"
		}
		body += pf.Markdown
	}
	if len(strings.TrimSpace(body)) == 0 {
		return nil, nil, false
	}
	opt := &gitlab.CreateIssueNoteOptions{
		Body:      gitlab.String(body),
		CreatedAt: createdAt,
	}
	return opt, options, true
}

func lhAttachmentToUploadFile(lhAttachment *lhAttachment) (string, []gitlab.OptionFunc, bool) {
	options := withSudoByUserID(lhAttachment.UploaderID)
	return lhAttachment.filename, options, true
}

func lhTicketToLabels(lhTicket *lhTicket, stateKey string) gitlab.Labels {
	var labels gitlab.Labels
	for _, tag := range lhTicket.Tags {
		labels = append(labels, tag.Tag.Name)
	}
	labels = append(labels, strings.Join([]string{stateKey, lhTicket.State}, "::"))
	return labels
}

func lhTicketVersionToLabels(lhVersion *tickets.TicketVersion, stateKey string) gitlab.Labels {
	var labels gitlab.Labels
	r := strings.NewReader(lhVersion.Tag)
	cr := csv.NewReader(r)
	cr.Comma = ' '
	record, err := cr.Read()
	if err != nil {
		record = strings.Fields(lhVersion.Tag)
	}
	for _, r := range record {
		if len(r) == 0 {
			continue
		}
		labels = append(labels, r)
	}
	labels = append(labels, strings.Join([]string{stateKey, lhVersion.State}, "::"))
	return labels
}

var lhCodeSpanRegexp = regexp.MustCompile(`@([^@\s][^@\r\n]*[^@\s])@`)

func lhtoGitLabMarkdown(text string) string {
	if len(strings.TrimSpace(text)) == 0 {
		return text
	}

	text = strings.ReplaceAll(text, `@@@`, "```")

	buf := &strings.Builder{}
	matches := lhCodeSpanRegexp.FindAllStringSubmatchIndex(text, -1)

	if matches == nil {
		return text
	}

	prev := 0
	for i, m := range matches {
		buf.WriteString(text[prev:m[0]])
		buf.WriteString("`" + text[m[2]:m[3]] + "`")
		if i == len(matches)-1 {
			buf.WriteString(text[m[1]:])
		}

		prev = m[1]
	}

	return buf.String()
}

type lhExport struct {
	plan     *lighthouse.Plan
	profile  *profiles.User
	projects *lhProjects
	users    *lhUsers
}

type lhProjects struct {
	list []*lhProject
}

type lhProject struct {
	*projects.Project

	memberships projects.Memberships
	milestones  lhMilestones
	tickets     lhTickets
}

type lhMilestones struct {
	list []*milestones.Milestone
}

type lhTickets struct {
	list []*lhTicket
}

type lhTicket struct {
	*tickets.Ticket

	attachments lhAttachments
}

type lhUsers struct {
	list []*lhUser
}

type lhUser struct {
	*users.User

	avatar      *lhFile
	memberships users.Memberships
}

type lhAttachments struct {
	list []*lhAttachment
}

type lhAttachment struct {
	*tickets.Attachment

	filename string
}

type lhFile struct {
	filename string
	r        io.Reader
}

func readLHExport(path string) (e *lhExport, tempDir string, err error) {
	tempDir, err = ioutil.TempDir("", "lhtogitlab")
	if err != nil {
		return nil, "", err
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	defer signal.Reset(os.Interrupt)

	go func(c chan os.Signal) {
		<-c
		signal.Reset(os.Interrupt)
		if len(tempDir) > 0 {
			os.RemoveAll(tempDir)
		}
	}(c)

	defer func() {
		if err != nil && len(tempDir) > 0 {
			os.RemoveAll(tempDir)
		}
	}()

	e = &lhExport{
		projects: &lhProjects{
			list: []*lhProject{},
		},
		users: &lhUsers{
			list: []*lhUser{},
		},
	}

	tgz := archiver.NewTarGz()
	tgz.Tar.OverwriteExisting = true

	err = tgz.Unarchive(path, tempDir)
	if err != nil {
		return nil, "", err
	}

	userDirs, err := filepath.Glob(filepath.Join(tempDir, "*", "users", "*"))
	if err != nil {
		return nil, "", err
	}

	for _, dir := range userDirs {
		uf, err := os.Open(filepath.Join(dir, "user.json"))
		if err != nil {
			return nil, "", err
		}
		defer uf.Close()
		dec := json.NewDecoder(uf)
		u := &lhUser{
			User:        &users.User{},
			memberships: users.Memberships{},
		}
		err = dec.Decode(u.User)
		if err != nil {
			return nil, "", err
		}
		uf.Close()
		mf, err := os.Open(filepath.Join(dir, "memberships.json"))
		if err == nil {
			defer mf.Close()
			dec = json.NewDecoder(mf)
			err = dec.Decode(&u.memberships)
			if err != nil {
				return nil, "", err
			}
			mf.Close()
		}
		avatarPaths, err := filepath.Glob(filepath.Join(dir, "avatar.*"))
		if err != nil {
			return nil, "", err
		}
		if len(avatarPaths) != 0 {
			u.avatar = &lhFile{
				filename: filepath.Base(avatarPaths[0]),
			}
			buf, err := ioutil.ReadFile(avatarPaths[0])
			if err != nil {
				return nil, "", err
			}
			u.avatar.r = bytes.NewReader(buf)
		}
		e.users.list = append(e.users.list, u)
	}
	sort.Slice(e.users.list, func(i, j int) bool { return e.users.list[i].ID < e.users.list[j].ID })

	projectDirs, err := filepath.Glob(filepath.Join(tempDir, "*", "projects", "*"))
	if err != nil {
		return nil, "", err
	}

	for _, dir := range projectDirs {
		pf, err := os.Open(filepath.Join(dir, "project.json"))
		if err != nil {
			return nil, "", err
		}
		defer pf.Close()
		dec := json.NewDecoder(pf)
		p := &lhProject{
			Project:     &projects.Project{},
			memberships: projects.Memberships{},
			milestones: lhMilestones{
				list: []*milestones.Milestone{},
			},
			tickets: lhTickets{
				list: []*lhTicket{},
			},
		}
		err = dec.Decode(p.Project)
		if err != nil {
			return nil, "", err
		}
		pf.Close()
		mf, err := os.Open(filepath.Join(dir, "memberships.json"))
		if err == nil {
			defer mf.Close()
			var memberships projects.Memberships
			dec = json.NewDecoder(mf)
			err = dec.Decode(&memberships)
			if err != nil {
				return nil, "", err
			}
			mf.Close()
			var unique projects.Memberships
			seen := map[int]struct{}{}
			for _, membership := range memberships {
				if _, ok := seen[membership.UserID]; ok {
					continue
				}
				unique = append(unique, membership)
				seen[membership.UserID] = struct{}{}
			}
			p.memberships = unique
		}

		milestonePaths, err := filepath.Glob(filepath.Join(dir, "milestones", "*.json"))
		if err != nil {
			return nil, "", err
		}
		for _, milestonePath := range milestonePaths {
			mf, err := os.Open(milestonePath)
			if err != nil {
				return nil, "", err
			}
			defer mf.Close()
			dec = json.NewDecoder(mf)
			m := &milestones.Milestone{}
			err = dec.Decode(m)
			if err != nil {
				return nil, "", err
			}
			mf.Close()
			p.milestones.list = append(p.milestones.list, m)
		}
		sort.Slice(p.milestones.list, func(i, j int) bool { return p.milestones.list[i].ID < p.milestones.list[j].ID })

		ticketDirs, err := filepath.Glob(filepath.Join(dir, "tickets", "*"))
		if err != nil {
			return nil, "", err
		}
		for _, ticketDir := range ticketDirs {
			tf, err := os.Open(filepath.Join(ticketDir, "ticket.json"))
			if err != nil {
				return nil, "", err
			}
			defer tf.Close()
			dec := json.NewDecoder(tf)
			t := &lhTicket{
				Ticket: &tickets.Ticket{},
				attachments: lhAttachments{
					list: []*lhAttachment{},
				},
			}
			err = dec.Decode(t.Ticket)
			if err != nil {
				return nil, "", err
			}
			tf.Close()
			filenameMap := map[string]*tickets.Attachment{}
			for _, a := range t.Attachments {
				filenameMap[a.Attachment.Filename] = a.Attachment
			}
			attachmentPaths, err := filepath.Glob(filepath.Join(ticketDir, "*"))
			if err != nil {
				return nil, "", err
			}
			for _, attachmentPath := range attachmentPaths {
				if filepath.Base(attachmentPath) == "ticket.json" {
					continue
				}
				a, ok := filenameMap[filepath.Base(attachmentPath)]
				if !ok {
					continue
				}
				attachment := &lhAttachment{
					Attachment: a,
					filename:   attachmentPath,
				}
				t.attachments.list = append(t.attachments.list, attachment)
			}
			p.tickets.list = append(p.tickets.list, t)
		}
		sort.Slice(p.tickets.list, func(i, j int) bool { return p.tickets.list[i].Number < p.tickets.list[j].Number })

		e.projects.list = append(e.projects.list, p)
	}
	sort.Slice(e.projects.list, func(i, j int) bool { return e.projects.list[i].ID < e.projects.list[j].ID })

	return e, tempDir, nil
}
