// Package source maps resource addresses from a plan to the file and line
// where they are declared, so changes can be linked and annotated in code
// review. JSON plans carry no source positions, so we parse the HCL.
package source

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/tofu-contrib/terraform-plan-review/internal/plan"
)

// Location is a position in the repository. File is relative to the
// repository root and uses forward slashes.
type Location struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// Index resolves configuration addresses to locations.
type Index struct {
	// blocks maps "<module path>|<address>" to the declaring block, where
	// module path is the dotted list of module call names ("" for root) and
	// address is relative to that module, without instance keys.
	blocks map[string]Location
}

// Build indexes the root module in dir and every local module it calls
// (sources starting with ./ or ../). Remote modules are not in the
// repository and are skipped. repoRoot is used to relativize file paths.
func Build(dir, repoRoot string, root plan.Module) (*Index, error) {
	idx := &Index{blocks: map[string]Location{}}
	if err := idx.add(dir, repoRoot, "", root); err != nil {
		return nil, err
	}
	return idx, nil
}

func (idx *Index) add(dir, repoRoot, modPath string, mod plan.Module) error {
	if err := idx.scanDir(dir, repoRoot, modPath); err != nil {
		return err
	}
	names := make([]string, 0, len(mod.ModuleCalls))
	for name := range mod.ModuleCalls {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		call := mod.ModuleCalls[name]
		if !strings.HasPrefix(call.Source, "./") && !strings.HasPrefix(call.Source, "../") {
			continue
		}
		child := name
		if modPath != "" {
			child = modPath + "." + name
		}
		if err := idx.add(filepath.Join(dir, call.Source), repoRoot, child, call.Module); err != nil {
			return err
		}
	}
	return nil
}

func (idx *Index) scanDir(dir, repoRoot, modPath string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".tf") || strings.HasSuffix(name, ".tofu")) {
			continue
		}
		path := filepath.Join(dir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel := path
		if r, err := filepath.Rel(repoRoot, path); err == nil {
			rel = r
		}
		rel = filepath.ToSlash(rel)
		// Parse errors are not fatal: tofu already validated the
		// configuration, and a partial index only costs us some links.
		file, _ := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
		if file == nil {
			continue
		}
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, b := range body.Blocks {
			idx.addBlock(b, modPath, rel, name)
		}
	}
	return nil
}

func (idx *Index) addBlock(b *hclsyntax.Block, modPath, file, name string) {
	loc := Location{File: file, Line: b.DefRange().Start.Line}
	switch b.Type {
	case "resource":
		if len(b.Labels) == 2 {
			idx.set(modPath, b.Labels[0]+"."+b.Labels[1], loc, name)
		}
	case "data":
		if len(b.Labels) == 2 {
			idx.set(modPath, "data."+b.Labels[0]+"."+b.Labels[1], loc, name)
		}
	case "removed":
		if addr := traversalAttr(b, "from"); addr != "" {
			idx.set(modPath, "removed:"+addr, loc, name)
		}
	case "import":
		if addr := traversalAttr(b, "to"); addr != "" {
			idx.set(modPath, "import:"+addr, loc, name)
		}
	}
}

// set records a location. OpenTofu gives .tofu files precedence over .tf
// files with the same name, so a .tofu declaration wins.
func (idx *Index) set(modPath, addr string, loc Location, name string) {
	key := modPath + "|" + addr
	if _, exists := idx.blocks[key]; exists && !strings.HasSuffix(name, ".tofu") {
		return
	}
	idx.blocks[key] = loc
}

func traversalAttr(b *hclsyntax.Block, name string) string {
	attr, ok := b.Body.Attributes[name]
	if !ok {
		return ""
	}
	trav, diags := hcl.AbsTraversalForExpr(attr.Expr)
	if diags.HasErrors() {
		return ""
	}
	var parts []string
	for _, step := range trav {
		switch s := step.(type) {
		case hcl.TraverseRoot:
			parts = append(parts, s.Name)
		case hcl.TraverseAttr:
			parts = append(parts, s.Name)
		}
	}
	return strings.Join(parts, ".")
}

// Lookup returns the declaration of a resource change. For resources
// being forgotten or imported it falls back to the removed/import block.
func (idx *Index) Lookup(rc plan.ResourceChange) (Location, bool) {
	if idx == nil {
		return Location{}, false
	}
	modPath := modulePath(rc.ModuleAddress)
	addr := rc.Type + "." + rc.Name
	if rc.Mode == "data" {
		addr = "data." + addr
	}
	if loc, ok := idx.blocks[modPath+"|"+addr]; ok {
		return loc, true
	}
	if loc, ok := idx.blocks[modPath+"|removed:"+addr]; ok {
		return loc, true
	}
	if loc, ok := idx.blocks[modPath+"|import:"+addr]; ok {
		return loc, true
	}
	// removed/import blocks in the root may target resources in modules.
	full := StripKeys(rc.Address)
	for _, kind := range []string{"removed:", "import:"} {
		if loc, ok := idx.blocks["|"+kind+full]; ok {
			return loc, true
		}
	}
	return Location{}, false
}

// modulePath turns `module.a[0].module.b["x"]` into "a.b".
func modulePath(moduleAddr string) string {
	if moduleAddr == "" {
		return ""
	}
	var names []string
	parts := strings.Split(StripKeys(moduleAddr), ".")
	for i := 0; i+1 < len(parts); i += 2 {
		if parts[i] == "module" {
			names = append(names, parts[i+1])
		}
	}
	return strings.Join(names, ".")
}

// StripKeys removes instance keys ([0], ["name"]) from an address.
func StripKeys(addr string) string {
	var sb strings.Builder
	depth := 0
	inString := false
	for i := 0; i < len(addr); i++ {
		c := addr[i]
		switch {
		case inString:
			if c == '\\' {
				i++
			} else if c == '"' {
				inString = false
			}
		case c == '[':
			depth++
		case c == ']':
			depth--
		case depth > 0:
			if c == '"' {
				inString = true
			}
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}
