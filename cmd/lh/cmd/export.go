package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nwidger/lighthouse/bins"
	"github.com/nwidger/lighthouse/changesets"
	"github.com/nwidger/lighthouse/messages"
	"github.com/nwidger/lighthouse/milestones"
	"github.com/nwidger/lighthouse/profiles"
	"github.com/nwidger/lighthouse/projects"
	"github.com/nwidger/lighthouse/tickets"
	"github.com/spf13/cobra"
)

type exportCmdOpts struct {
	attachments bool
}

var exportCmdFlags exportCmdOpts

// exportCmd represents the export command
var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export Lighthouse account data",
	Long: `Export Lighthouse account data

Export will be written to the current directory with filename
ACCOUNT_YYYY-MM-DD.tar.gz.  If export fails due to issuing too many
API requests, consider using -r and -b to rate limit API requests.

`,
	Run: func(cmd *cobra.Command, args []string) {
		flags := exportCmdFlags
		_ = flags

		account := Account()
		base := filepath.Join(".", account)

		exportFilename := fmt.Sprintf(`%s_%s.tar.gz`, account, time.Now().Format(`2006-01-02`))
		fmt.Println("writing export file to", exportFilename)

		f, err := os.Create(exportFilename)
		if err != nil {
			FatalUsage(cmd, err)
		}
		defer f.Close()
		z := gzip.NewWriter(f)
		defer z.Close()
		tw := tar.NewWriter(z)
		defer tw.Close()

		fatalUsage := func(cmd *cobra.Command, v ...interface{}) {
			tw.Close()
			z.Close()
			f.Close()
			FatalUsage(cmd, v...)
		}

		writeDir(cmd, tw, base)

		// account plan (only works if you are the account
		// owner, don't consider it an error if this fails)
		plan, err := service.Plan()
		if err == nil {
			writeJSONFile(cmd, tw, filepath.Join(base, "plan.json"), plan)
		}

		// account profile
		pp := profiles.NewService(service)
		up, err := pp.Get()
		if err == nil {
			writeJSONFile(cmd, tw, filepath.Join(base, "profile.json"), up)
		}

		// account projects
		p := projects.NewService(service)
		ps, err := p.List()
		if err != nil {
			fatalUsage(cmd, err)
		}
		for _, project := range ps {
			projectBase := filepath.Join(base, "projects", filename(fmt.Sprintf("%d-%s", project.ID, project.Permalink)))
			writeDir(cmd, tw, projectBase)

			// project metadata
			writeJSONFile(cmd, tw, filepath.Join(projectBase, "project.json"), project)

			// project memberships
			memberships, err := p.MembershipsByID(project.ID)
			if err != nil {
				fatalUsage(cmd, err)
			}
			writeJSONFile(cmd, tw, filepath.Join(projectBase, "memberships.json"), memberships)

			// project bins
			binsBase := filepath.Join(projectBase, "bins")
			b := bins.NewService(service, project.ID)
			bs, err := b.List()
			if err != nil {
				fatalUsage(cmd, err)
			}
			writeDir(cmd, tw, binsBase)
			for _, bin := range bs {
				writeJSONFile(cmd, tw, filepath.Join(binsBase, filename(fmt.Sprintf("%d-%s.json", bin.ID, bin.Name))), bin)
			}

			// project changesets
			changesetsBase := filepath.Join(projectBase, "changesets")
			c := changesets.NewService(service, project.ID)
			cs, err := c.List()
			if err != nil {
				fatalUsage(cmd, err)
			}
			writeDir(cmd, tw, changesetsBase)
			for _, changeset := range cs {
				writeJSONFile(cmd, tw, filepath.Join(changesetsBase, filename(fmt.Sprintf("%s.json", changeset.Revision))), changeset)
			}

			// project messages
			messagesBase := filepath.Join(projectBase, "messages")
			mg := messages.NewService(service, project.ID)
			mgs, err := mg.List()
			if err != nil {
				fatalUsage(cmd, err)
			}
			writeDir(cmd, tw, messagesBase)
			for _, message := range mgs {
				writeJSONFile(cmd, tw, filepath.Join(messagesBase, filename(fmt.Sprintf("%d-%s.json", message.ID, message.Permalink))), message)
			}

			// project milestones
			milestonesBase := filepath.Join(projectBase, "milestones")
			m := milestones.NewService(service, project.ID)
			ms, err := m.ListAll(nil)
			if err != nil {
				fatalUsage(cmd, err)
			}
			writeDir(cmd, tw, milestonesBase)
			for _, milestone := range ms {
				writeJSONFile(cmd, tw, filepath.Join(milestonesBase, filename(fmt.Sprintf("%d-%s.json", milestone.ID, milestone.Permalink))), milestone)
			}

			// project tickets
			t := tickets.NewService(service, project.ID)
			opts := &tickets.ListOptions{
				Limit: tickets.MaxLimit,
			}
			ticketsBase := filepath.Join(projectBase, "tickets")
			writeDir(cmd, tw, ticketsBase)
			for opts.Page = 1; ; opts.Page++ {
				ts, err := t.List(opts)
				if err != nil {
					fatalUsage(cmd, err)
				}
				if len(ts) == 0 {
					break
				}
				for _, ticket := range ts {
					ticketBase := filepath.Join(ticketsBase, filename(fmt.Sprintf("%d-%s", ticket.Number, ticket.Permalink)))
					writeJSONFile(cmd, tw, filepath.Join(ticketBase, "ticket.json"), ticket)

					if !flags.attachments {
						continue
					}

					// ticket attachments
					for _, attachment := range ticket.Attachments {
						rc, err := t.GetAttachment(attachment.Attachment)
						if err != nil {
							fatalUsage(cmd, err)
						}
						buf, err := ioutil.ReadAll(rc)
						if err != nil {
							fatalUsage(cmd, err)
						}
						writeFile(cmd, tw, filepath.Join(ticketBase, attachment.Attachment.Filename), buf)
					}
				}
			}
		}
	},
}

func filename(name string) string {
	if len(name) > 20 {
		name = name[:20]
	}
	name = strings.ToLower(strings.TrimSpace(name))
	re, err := regexp.Compile(`[^-a-z0-9_]+`)
	if err != nil {
		return name
	}
	sep := `-`
	name = re.ReplaceAllString(name, sep)
	re, err = regexp.Compile(sep + `+`)
	if err != nil {
		return name
	}
	name = re.ReplaceAllString(name, sep)
	name = strings.TrimRight(name, sep)
	return name
}

func writeJSONFile(cmd *cobra.Command, tw *tar.Writer, filename string, v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		FatalUsage(cmd, err)
	}
	writeFile(cmd, tw, filename, data)
}

func writeDir(cmd *cobra.Command, tw *tar.Writer, dirname string) {
	hdr := &tar.Header{
		Typeflag: tar.TypeDir,
		Name:     dirname,
		Mode:     0755,
		Uid:      1000,
		Gid:      1000,
	}
	err := tw.WriteHeader(hdr)
	if err != nil {
		FatalUsage(cmd, err)
	}
}

func writeFile(cmd *cobra.Command, tw *tar.Writer, filename string, data []byte) {
	fmt.Fprintln(os.Stderr, filename)
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     filename,
		Size:     int64(len(data)),
		Mode:     0644,
		Uid:      1000,
		Gid:      1000,
	}
	err := tw.WriteHeader(hdr)
	if err != nil {
		FatalUsage(cmd, err)
	}
	_, err = io.Copy(tw, bytes.NewReader(data))
	if err != nil {
		FatalUsage(cmd, err)
	}
}

func init() {
	RootCmd.AddCommand(exportCmd)
	exportCmd.Flags().BoolVar(&exportCmdFlags.attachments, "attachments", false, "Include attachments in export")
}