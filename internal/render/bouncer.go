package render

import (
	"fmt"
	"io/fs"
	"net"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/chr0nzz/tm-cli/internal/answers"
)

const (
	BouncerModule     = "github.com/maxlerebourg/crowdsec-bouncer-traefik-plugin"
	BouncerVersion    = "v1.7.1"
	BouncerAlias      = "crowdsec"
	BouncerMiddleware = "crowdsec"
	BouncerFileName   = "crowdsec.yml"
	BouncerRef        = BouncerMiddleware + "@file"
	BouncerBackupExt  = ".tm.bak"
	TraefikStaticRel  = "traefik/traefik.yml"

	bouncerCrowdSecMode = "live"
	lapiDefaultPort     = "8080"
)

type Static struct {
	Path       string
	Alias      string
	TrustedIPs []string
	Merged     string
	Mode       fs.FileMode
	Ready      bool
	Note       string
}

type BouncerPlan struct {
	On             bool
	Enabled        bool
	Alias          string
	StaticPath     string
	StaticMerged   string
	StaticMode     fs.FileMode
	MiddlewarePath string
	Inline         bool
	Fill           bool
	LapiHost       string
	LapiScheme     string
	TrustedIPs     []string
	Note           string
}

type pluginView struct {
	Alias   string
	Module  string
	Version string
}

type bouncerView struct {
	Name       string
	Alias      string
	Mode       string
	Scheme     string
	Host       string
	Key        string
	TrustedIPs []string
}

func ParseStatic(data []byte) (Static, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Static{}, err
	}
	root := plainNode(&doc)
	if root == nil {
		return Static{}, nil
	}
	if root.Kind != yaml.MappingNode {
		return Static{}, fmt.Errorf("the root of the static config is not a mapping")
	}
	var s Static
	seen := map[string]bool{}
	entries := plainNode(mapValue(root, "entryPoints"))
	if entries != nil && entries.Kind == yaml.MappingNode {
		for i := 1; i < len(entries.Content); i += 2 {
			for _, ip := range scalarList(mapValue(mapValue(entries.Content[i], "forwardedHeaders"), "trustedIPs")) {
				if seen[ip] {
					continue
				}
				seen[ip] = true
				s.TrustedIPs = append(s.TrustedIPs, ip)
			}
		}
	}
	plugins := plainNode(mapValue(mapValue(root, "experimental"), "plugins"))
	if plugins != nil && plugins.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(plugins.Content); i += 2 {
			module := mapValue(plugins.Content[i+1], "moduleName")
			if module != nil && strings.TrimSpace(module.Value) == BouncerModule {
				s.Alias = plugins.Content[i].Value
				break
			}
		}
	}
	return s, nil
}

func StaticFrom(path string, data []byte) Static {
	s := Static{Path: path, Mode: 0o644}
	facts, err := ParseStatic(data)
	if err != nil {
		s.Note = "could not read " + path + " as yaml: " + err.Error()
		return s
	}
	s.Alias = facts.Alias
	s.TrustedIPs = facts.TrustedIPs
	if s.Alias != "" {
		s.Ready = true
		return s
	}
	merged, err := MergeStatic(data, BouncerAlias)
	if err != nil {
		s.Note = err.Error()
		return s
	}
	s.Alias = BouncerAlias
	s.Merged = merged
	s.Ready = true
	return s
}

func MergeStatic(data []byte, alias string) (string, error) {
	merged, err := splicePlugin(data, alias)
	if err != nil {
		return "", err
	}
	facts, err := ParseStatic([]byte(merged))
	if err != nil {
		return "", fmt.Errorf("adding the plugin would leave the static config unreadable (%w), so tm left it alone", err)
	}
	if facts.Alias != alias {
		return "", fmt.Errorf("tm could not place the plugin declaration in this static config, add it by hand")
	}
	return merged, nil
}

func splicePlugin(data []byte, alias string) (string, error) {
	lines := strings.Split(string(data), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	root := -1
	for i, line := range lines {
		if indentOf(line) == 0 && key(line) == "experimental" {
			root = i
			break
		}
	}
	if root < 0 {
		block := []string{"experimental:", "  plugins:"}
		block = append(block, pluginEntry(alias, 4, 2)...)
		if len(lines) > 0 {
			block = append([]string{""}, block...)
		}
		return textOf(append(lines, block...)), nil
	}
	if rest := after(lines[root]); rest != "" {
		return "", fmt.Errorf("experimental: on line %d is written inline as %q, so tm cannot add the plugin without rewriting the file", root+1, rest)
	}
	end := blockEnd(lines, root)
	step := indentStep(lines[root+1 : end])
	plugins := -1
	for i := root + 1; i < end; i++ {
		if indentOf(lines[i]) == step && key(lines[i]) == "plugins" {
			plugins = i
			break
		}
	}
	if plugins < 0 {
		block := append([]string{pad(step) + "plugins:"}, pluginEntry(alias, 2*step, step)...)
		return textOf(splice(lines, root+1, block)), nil
	}
	if rest := after(lines[plugins]); rest != "" {
		return "", fmt.Errorf("experimental.plugins on line %d is written inline as %q, so tm cannot add the plugin without rewriting the file", plugins+1, rest)
	}
	inner := indentStep(lines[plugins+1 : blockEnd(lines, plugins)])
	if inner <= step {
		inner = 2 * step
	}
	return textOf(splice(lines, plugins+1, pluginEntry(alias, inner, inner-step))), nil
}

func IsPlaceholder(data []byte) bool {
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	return emptyValue(doc, 2)
}

func emptyValue(v any, depth int) bool {
	switch t := v.(type) {
	case nil:
		return true
	case map[string]any:
		if len(t) == 0 {
			return true
		}
		if depth <= 0 {
			return false
		}
		for _, item := range t {
			if !emptyValue(item, depth-1) {
				return false
			}
		}
		return true
	case []any:
		return len(t) == 0
	case string:
		return strings.TrimSpace(t) == ""
	}
	return false
}

func Bouncer(a *answers.Answers, st Static) BouncerPlan {
	p := BouncerPlan{On: a.CrowdSec.BouncerPlugin}
	if !p.On {
		return p
	}
	p.Alias = BouncerAlias
	p.LapiHost, p.LapiScheme = lapiEndpoint(a.CrowdSec.LAPIURL)
	if a.Mode.HasTraefik() {
		p.Enabled = true
		p.StaticPath = generatedStaticPath(a)
	} else {
		if !st.Ready {
			p.Note = st.Note
			if p.Note == "" {
				p.Note = "the existing " + a.Mounts.StaticConfigPath + " could not be read"
			}
			return p
		}
		p.Enabled = true
		p.Alias = st.Alias
		p.StaticPath = st.Path
		p.StaticMerged = st.Merged
		p.StaticMode = st.Mode
		p.TrustedIPs = st.TrustedIPs
	}
	if p.StaticMode == 0 {
		p.StaticMode = 0o644
	}
	single := a.Config.Layout == answers.LayoutSingle
	switch a.Mode {
	case answers.ModeFull, answers.ModeAgentDockerTraefik:
		if single {
			p.MiddlewarePath, p.Inline = "traefik/config/dynamic.yml", true
		} else {
			p.MiddlewarePath = "traefik/config/" + BouncerFileName
		}
	case answers.ModeFullNative:
		if single {
			p.MiddlewarePath, p.Inline = a.Config.Path, true
		} else {
			p.MiddlewarePath = filepath.Join(a.Config.Dir, BouncerFileName)
		}
	case answers.ModeTMDocker:
		if single {
			p.MiddlewarePath, p.Inline = "config/dynamic.yml", true
		} else {
			p.MiddlewarePath = "config/" + BouncerFileName
		}
	case answers.ModeTMNative:
		if single {
			p.Note = "the dynamic config is a single file this install did not create, so the middleware was not written into it"
		} else {
			p.MiddlewarePath, p.Fill = filepath.Join(a.Config.Dir, BouncerFileName), true
		}
	case answers.ModeAgentDocker, answers.ModeAgentBinary:
		p.MiddlewarePath, p.Fill = filepath.Join(a.Agent.ConfigPath, BouncerFileName), true
	}
	return p
}

func MiddlewareYAML(a *answers.Answers, p BouncerPlan) string {
	out, err := execute("crowdsec.yml.tmpl", newBouncerView(a, p))
	if err != nil {
		return ""
	}
	return out
}

func generatedStaticPath(a *answers.Answers) string {
	if a.Mode == answers.ModeFullNative {
		return answers.DefaultStaticConfigPath
	}
	return TraefikStaticRel
}

func lapiEndpoint(url string) (string, string) {
	scheme := "http"
	s := strings.TrimSpace(url)
	if rest, ok := strings.CutPrefix(s, "https://"); ok {
		scheme, s = "https", rest
	} else if rest, ok := strings.CutPrefix(s, "http://"); ok {
		s = rest
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "", scheme
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		s = net.JoinHostPort(s, lapiDefaultPort)
	}
	return s, scheme
}

func newBouncerView(a *answers.Answers, p BouncerPlan) *bouncerView {
	return &bouncerView{
		Name:       BouncerMiddleware,
		Alias:      p.Alias,
		Mode:       bouncerCrowdSecMode,
		Scheme:     p.LapiScheme,
		Host:       p.LapiHost,
		Key:        a.Secrets[answers.SecretCrowdSecAPIKey],
		TrustedIPs: p.TrustedIPs,
	}
}

func newPluginView(p BouncerPlan) *pluginView {
	return &pluginView{Alias: p.Alias, Module: BouncerModule, Version: BouncerVersion}
}

func plainNode(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	return n
}

func mapValue(n *yaml.Node, name string) *yaml.Node {
	n = plainNode(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if strings.EqualFold(n.Content[i].Value, name) {
			return n.Content[i+1]
		}
	}
	return nil
}

func scalarList(n *yaml.Node) []string {
	n = plainNode(n)
	if n == nil {
		return nil
	}
	var out []string
	add := func(v string) {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	if n.Kind == yaml.ScalarNode {
		add(n.Value)
		return out
	}
	if n.Kind != yaml.SequenceNode {
		return nil
	}
	for _, item := range n.Content {
		if item.Kind == yaml.ScalarNode {
			add(item.Value)
		}
	}
	return out
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func key(line string) string {
	if strings.HasPrefix(strings.TrimLeft(line, " "), "#") {
		return ""
	}
	name, _, ok := strings.Cut(line, ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(name)
}

func after(line string) string {
	_, rest, ok := strings.Cut(line, ":")
	if !ok {
		return ""
	}
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "#") {
		return ""
	}
	return rest
}

func blockEnd(lines []string, start int) int {
	base := indentOf(lines[start])
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if indentOf(lines[i]) <= base {
			return i
		}
	}
	return len(lines)
}

func indentStep(block []string) int {
	for _, line := range block {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if n := indentOf(line); n > 0 {
			return n
		}
	}
	return 2
}

func pad(n int) string {
	return strings.Repeat(" ", n)
}

func pluginEntry(alias string, indent, step int) []string {
	if step < 1 {
		step = 2
	}
	return []string{
		pad(indent) + alias + ":",
		pad(indent+step) + "moduleName: " + BouncerModule,
		pad(indent+step) + "version: " + BouncerVersion,
	}
}

func splice(lines []string, at int, block []string) []string {
	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:at]...)
	out = append(out, block...)
	out = append(out, lines[at:]...)
	return out
}

func textOf(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
