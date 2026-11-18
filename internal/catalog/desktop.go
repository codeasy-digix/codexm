package catalog

// Desktop metadata is a read-only compatibility projection. It never imports,
// repairs or writes native rows, GUI preferences or account-server settings.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

type ProjectRoot struct {
	Path string `json:"path"`
}
type DesktopProject struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Roots    []ProjectRoot `json:"roots"`
	NativeID string        `json:"nativeId,omitempty"`
}
type Desktop struct {
	Projects           []DesktopProject
	AdditionalProjects []DesktopProject
	PinnedSection      string
	LegacyPinned       []string
	Titles             map[string]string
	ChatSort           string
	assignments        map[string]projectAssignment
	hosts              map[string]string
	projectless        []string
	hints              map[string]string
	aliases            map[string]string
}
type projectAssignment struct {
	Kind string `json:"projectKind"`
	ID   string `json:"projectId"`
}

func ReadDesktop(ctx context.Context, home string) (Desktop, error) {
	d := Desktop{Projects: []DesktopProject{}, ChatSort: "updated_at", aliases: map[string]string{}, hints: map[string]string{}}
	filename, err := database(home)
	if err != nil {
		return d, err
	}
	native := []DesktopProject{}
	if filename != "" {
		db, err := OpenReadOnly(filename)
		if err != nil {
			return d, err
		}
		defer db.Close()
		var exists int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='projects'`).Scan(&exists); err != nil {
			return d, err
		}
		if exists != 0 {
			rows, err := db.QueryContext(ctx, `SELECT id,name FROM projects ORDER BY position,id`)
			if err != nil {
				return d, err
			}
			for rows.Next() {
				var p DesktopProject
				if err = rows.Scan(&p.ID, &p.Name); err != nil {
					rows.Close()
					return d, err
				}
				p.NativeID = p.ID
				p.Roots = []ProjectRoot{}
				native = append(native, p)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return d, err
			}
			for i := range native {
				roots, err := db.QueryContext(ctx, `SELECT path FROM project_roots WHERE project_id=? ORDER BY position`, native[i].ID)
				if err != nil {
					return d, err
				}
				for roots.Next() {
					var r ProjectRoot
					if err = roots.Scan(&r.Path); err != nil {
						roots.Close()
						return d, err
					}
					native[i].Roots = append(native[i].Roots, r)
				}
				err = roots.Err()
				roots.Close()
				if err != nil {
					return d, err
				}
			}
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='thread_sections'`).Scan(&exists); err != nil {
			return d, err
		}
		if exists != 0 {
			// The built-in identity stays stable even if its display name changes.
			rows, err := db.QueryContext(ctx, `SELECT id,name FROM thread_sections`)
			if err != nil {
				return d, err
			}
			for rows.Next() {
				var id, name string
				if err = rows.Scan(&id, &name); err != nil {
					rows.Close()
					return d, err
				}
				if id == "01984de2-8f74-7c91-a3b2-5c5e937cf318" || (d.PinnedSection == "" && name == "Pinned") {
					d.PinnedSection = id
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return d, err
			}
		}
	}
	var state struct {
		Projects map[string]struct {
			ID    string   `json:"id"`
			Name  string   `json:"name"`
			Roots []string `json:"rootPaths"`
		} `json:"local-projects"`
		Order       []string                     `json:"project-order"`
		Mappings    map[string]map[string]string `json:"app-server-project-id-by-legacy-project-id-by-host"`
		Assignments map[string]projectAssignment `json:"thread-project-assignments"`
		Hosts       map[string]string            `json:"thread-project-membership-host-ids"`
		Projectless []string                     `json:"projectless-thread-ids"`
		Pinned      []string                     `json:"pinned-thread-ids"`
		Titles      struct {
			Titles map[string]string `json:"titles"`
		} `json:"thread-titles"`
		Owner string `json:"electron-renderer-content-owner"`
	}
	// Decode only the allowlisted metadata fields; never read credentials or
	// expose arbitrary GUI state to the browser. A corrupt snapshot is an error.
	f, err := os.Open(filepath.Join(home, ".codex-global-state.json"))
	var raw map[string]json.RawMessage
	if err == nil {
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, 16*1024*1024+1))
		if e != nil {
			return d, e
		}
		if len(b) > 16*1024*1024 {
			return d, fmt.Errorf("GUI metadata exceeds 16 MiB")
		}
		if e = json.Unmarshal(b, &state); e != nil {
			return d, fmt.Errorf("cannot read GUI project metadata: %w", e)
		}
		_ = json.Unmarshal(b, &raw)
	} else if !os.IsNotExist(err) {
		return d, err
	}
	d.assignments, d.hosts, d.projectless, d.LegacyPinned, d.Titles = state.Assignments, state.Hosts, state.Projectless, state.Pinned, state.Titles.Titles
	var atoms struct {
		Preferences struct {
			ChatSort string `json:"chatSortMode"`
		} `json:"flat-project-sidebar-preferences-v1"`
	}
	if state.Owner != "" {
		_ = json.Unmarshal(raw["electron-owner-atoms:"+state.Owner], &atoms)
		var hints map[string]string
		_ = json.Unmarshal(raw["electron-owner-value:thread-workspace-root-hints:"+state.Owner], &hints)
		for id, path := range hints {
			d.hints[id] = path
		}
	}
	if slices.Contains([]string{"updated_at", "created_at", "recency_at"}, atoms.Preferences.ChatSort) {
		d.ChatSort = atoms.Preferences.ChatSort
	}
	mapping := state.Mappings["local:"+filepath.Clean(home)]
	byNative := map[string]DesktopProject{}
	for _, p := range native {
		byNative[p.ID] = p
	}
	order := append([]string{}, state.Order...)
	extra := []string{}
	for id := range state.Projects {
		if !slices.Contains(order, id) {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)
	seen := map[string]bool{}
	for _, id := range order {
		p, ok := state.Projects[id]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		nid := mapping[id]
		np := byNative[nid]
		roots := []ProjectRoot{}
		for _, r := range p.Roots {
			roots = append(roots, ProjectRoot{r})
		}
		if len(roots) == 0 {
			roots = np.Roots
		}
		name := p.Name
		if name == "" {
			name = np.Name
		}
		d.Projects = append(d.Projects, DesktopProject{ID: id, Name: name, Roots: roots, NativeID: nid})
		d.aliases[id] = id
		if nid != "" {
			d.aliases[nid] = id
		}
	}
	// Native-only projects (e.g. created by an official CLI) remain accessible;
	// imported legacy projects are represented once, under their GUI identity.
	for _, p := range native {
		if d.aliases[p.ID] != "" {
			continue
		}
		if state.Projects != nil {
			d.AdditionalProjects = append(d.AdditionalProjects, p)
		} else {
			d.Projects = append(d.Projects, p)
		}
		d.aliases[p.ID] = p.ID
	}
	return d, nil
}

func (d Desktop) ProjectFor(e Entry) string {
	if e.ProjectID != "" {
		if id := d.aliases[e.ProjectID]; id != "" {
			return id
		}
		return ""
	}
	if slices.Contains(d.projectless, e.ID) {
		return ""
	}
	if a, ok := d.assignments[e.ID]; ok {
		if a.Kind != "local" || (d.hosts[e.ID] != "" && d.hosts[e.ID] != "local") {
			return ""
		}
		if id := d.aliases[a.ID]; id != "" {
			return id
		}
		return ""
	}
	path := e.Cwd
	if d.hints[e.ID] != "" {
		path = d.hints[e.ID]
	}
	if !filepath.IsAbs(path) {
		return ""
	}
	path = filepath.Clean(path)
	best, id := 0, ""
	for _, p := range d.Projects {
		for _, r := range p.Roots {
			root := filepath.Clean(r.Path)
			if filepath.IsAbs(root) && path == root && len(root) > best {
				best, id = len(root), p.ID
			}
		}
	}
	return id
}
func (d Desktop) NativeProject(id string) (string, error) {
	if id == "" {
		return "", nil
	}
	for _, p := range append(slices.Clone(d.Projects), d.AdditionalProjects...) {
		if p.ID == id || p.NativeID == id {
			if p.NativeID != "" {
				return p.NativeID, nil
			}
			return "", fmt.Errorf("this legacy GUI project has no native identity; open it in the GUI before changing its membership")
		}
	}
	return "", fmt.Errorf("project is not in this machine's catalog")
}
func (d Desktop) IsPinned(e Entry) bool {
	if d.PinnedSection != "" {
		return e.SectionID == d.PinnedSection
	}
	return slices.Contains(d.LegacyPinned, e.ID)
}

func (d Desktop) Title(e Entry) string {
	if e.nativeUntitled && d.Titles[e.ID] != "" {
		return d.Titles[e.ID]
	}
	return e.Title
}
