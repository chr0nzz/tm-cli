package state

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const composeFileName = "docker-compose.yml"

const (
	imageTM       = "traefik-manager"
	imageAgent    = "traefik-manager-agent"
	imageTraefik  = "traefik"
	imageCrowdSec = "crowdsec"
)

type compose struct {
	Services map[string]*service       `yaml:"services"`
	Networks map[string]composeNetwork `yaml:"networks"`
	Volumes  map[string]any            `yaml:"volumes"`
}

type composeNetwork struct {
	External flexBool `yaml:"external"`
	Internal flexBool `yaml:"internal"`
	Name     string   `yaml:"name"`
}

type flexBool bool

func (b *flexBool) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		*b = true
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(n.Value)) {
	case "true", "yes", "on", "1":
		*b = true
	default:
		*b = false
	}
	return nil
}

type service struct {
	Image         string         `yaml:"image"`
	ContainerName string         `yaml:"container_name"`
	Labels        kvList         `yaml:"labels"`
	Environment   kvList         `yaml:"environment"`
	Volumes       []mount        `yaml:"volumes"`
	Ports         []portMap      `yaml:"ports"`
	Networks      nameList       `yaml:"networks"`
	Healthcheck   map[string]any `yaml:"healthcheck"`
}

type kv struct {
	Key   string
	Value string
}

type kvList []kv

func (l *kvList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode {
				continue
			}
			k, v, _ := strings.Cut(item.Value, "=")
			*l = append(*l, kv{Key: strings.TrimSpace(k), Value: v})
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			val := n.Content[i+1]
			if val.Kind != yaml.ScalarNode {
				continue
			}
			v := val.Value
			if val.Tag == "!!null" {
				v = ""
			}
			*l = append(*l, kv{Key: n.Content[i].Value, Value: v})
		}
	}
	return nil
}

func (l kvList) get(key string) (string, bool) {
	for _, e := range l {
		if e.Key == key {
			return e.Value, true
		}
	}
	return "", false
}

func (l kvList) has(key string) bool {
	_, ok := l.get(key)
	return ok
}

func (l kvList) truthy(key string) bool {
	v, ok := l.get(key)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "on", "1":
		return true
	}
	return false
}

type mount struct {
	Source string
	Target string
}

func (m *mount) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		var long struct {
			Source string `yaml:"source"`
			Target string `yaml:"target"`
		}
		if err := n.Decode(&long); err != nil {
			return err
		}
		m.Source, m.Target = long.Source, long.Target
		return nil
	}
	parts := strings.Split(n.Value, ":")
	if len(parts) >= 2 {
		m.Source, m.Target = parts[0], parts[1]
	} else {
		m.Target = parts[0]
	}
	return nil
}

type portMap struct {
	Host      string
	Container string
}

func (p *portMap) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		var long struct {
			Target    any `yaml:"target"`
			Published any `yaml:"published"`
		}
		if err := n.Decode(&long); err != nil {
			return err
		}
		if long.Target != nil {
			p.Container = fmt.Sprint(long.Target)
		}
		if long.Published != nil {
			p.Host = fmt.Sprint(long.Published)
		}
		return nil
	}
	v := n.Value
	if i := strings.Index(v, "/"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ":")
	switch len(parts) {
	case 1:
		p.Container = parts[0]
	case 2:
		p.Host, p.Container = parts[0], parts[1]
	default:
		p.Host, p.Container = parts[len(parts)-2], parts[len(parts)-1]
	}
	return nil
}

type nameList []string

func (l *nameList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.SequenceNode:
		for _, item := range n.Content {
			if item.Kind == yaml.ScalarNode {
				*l = append(*l, item.Value)
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			*l = append(*l, n.Content[i].Value)
		}
	}
	return nil
}

func parseCompose(data []byte) (*compose, error) {
	var c compose
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func imageName(image string) string {
	name := image
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	slash := strings.LastIndex(name, "/")
	if i := strings.LastIndex(name, ":"); i > slash {
		name = name[:i]
	}
	if slash >= 0 {
		name = name[slash+1:]
	}
	return name
}

func (c *compose) find(name string) *service {
	for _, s := range c.Services {
		if s != nil && s.Image != "" && imageName(s.Image) == name {
			return s
		}
	}
	if s, ok := c.Services[name]; ok && s != nil {
		return s
	}
	for _, s := range c.Services {
		if s != nil && s.ContainerName == name {
			return s
		}
	}
	return nil
}

func (c *compose) network(name string) composeNetwork {
	if c.Networks == nil {
		return composeNetwork{}
	}
	return c.Networks[name]
}

func (s *service) env(key string) (string, bool) {
	if s == nil {
		return "", false
	}
	return s.Environment.get(key)
}

func (s *service) label(key string) (string, bool) {
	if s == nil {
		return "", false
	}
	return s.Labels.get(key)
}

func (s *service) hasLabelPrefix(prefix string) bool {
	if s == nil {
		return false
	}
	for _, l := range s.Labels {
		if strings.HasPrefix(l.Key, prefix) {
			return true
		}
	}
	return false
}

func (s *service) hasLabelSuffix(suffix string) bool {
	if s == nil {
		return false
	}
	for _, l := range s.Labels {
		if strings.HasSuffix(l.Key, suffix) {
			return true
		}
	}
	return false
}

func (s *service) mountSource(target string) (string, bool) {
	if s == nil {
		return "", false
	}
	want := path.Clean(target)
	for _, m := range s.Volumes {
		if m.Target != "" && path.Clean(m.Target) == want {
			return m.Source, true
		}
	}
	return "", false
}

func (s *service) hostPort(container string) string {
	if s == nil {
		return ""
	}
	for _, p := range s.Ports {
		if p.Container == container && p.Host != "" {
			return p.Host
		}
	}
	return ""
}

var hostRuleRe = regexp.MustCompile("Host\\(\\s*[`\"]([^`\"]+)[`\"]")

func (s *service) routerHost(router string) (string, bool) {
	rule, ok := s.label("traefik.http.routers." + router + ".rule")
	if !ok {
		return "", false
	}
	m := hostRuleRe.FindStringSubmatch(rule)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func isReference(v string) bool {
	return strings.HasPrefix(strings.TrimSpace(v), "$")
}

func ServiceNames(data []byte) ([]string, error) {
	c, err := parseCompose(data)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(c.Services))
	for name := range c.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func imageTag(image string) string {
	name := image
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	slash := strings.LastIndex(name, "/")
	if i := strings.LastIndex(name, ":"); i > slash {
		return name[i+1:]
	}
	return ""
}
