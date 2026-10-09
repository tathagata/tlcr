package core

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// yamlFlavor is the one classification every YAML-backed parser shares, so a
// repository mixing Ansible and Kubernetes files never has two parsers
// claiming the same file.
type yamlFlavor int

const (
	yamlPlain yamlFlavor = iota
	yamlKubernetes
	yamlAnsiblePlaybook
	yamlAnsibleTasks
)

const maxYAMLDocuments = 256

// inlineVaultRe matches the header of a `!vault |` block scalar.
var inlineVaultRe = regexp.MustCompile(`(?m)^[ \t]+\$ANSIBLE_VAULT;`)

// ansibleFQCNRe matches a fully qualified collection name such as
// `ansible.builtin.apt`.
var ansibleFQCNRe = regexp.MustCompile(`^[a-z0-9_]+\.[a-z0-9_]+\.[a-z0-9_.]+$`)

// ansibleTaskKeywords are the task and block keys that are not the action.
var ansibleTaskKeywords = wordSet(`always any_errors_fatal args async become become_exe become_flags
	become_method become_user block changed_when check_mode collections connection debugger delay
	delegate_facts delegate_to diff environment failed_when ignore_errors ignore_unreachable listen
	loop loop_control module_defaults name no_log notify poll port register remote_user rescue
	retries run_once tags throttle timeout until vars when`)

// ansibleActions are action keys recognized without a collection prefix.
var ansibleActions = wordSet(`action add_host apt apt_key apt_repository assemble assert async_status
	blockinfile command copy cron debconf debug dnf dnf5 dpkg_selections expect fail fetch file find
	gather_facts get_url getent git group group_by hostname import_role import_tasks include
	include_role include_tasks include_vars iptables known_hosts lineinfile local_action meta package
	package_facts pause ping pip raw reboot replace rpm_key script service service_facts set_fact
	set_stats setup shell slurp stat subversion systemd systemd_service sysvinit tempfile template
	unarchive uri user validate_argument_spec wait_for wait_for_connection yum yum_repository`)

func wordSet(words string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(words) {
		set[word] = true
	}
	return set
}

func isYAMLPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yml" || ext == ".yaml"
}

// vaultEncrypted reports Ansible Vault ciphertext: a wholly encrypted file of
// any type, or a YAML file carrying an inline `!vault` value.
func vaultEncrypted(path, source string) bool {
	if strings.HasPrefix(strings.TrimSpace(source), "$ANSIBLE_VAULT;") {
		return true
	}
	return isYAMLPath(path) && inlineVaultRe.MatchString(source)
}

// yamlRoots returns the root node of every non-empty document. Aliases are
// never expanded, so anchors cannot amplify the bounded input.
func yamlRoots(data []byte) ([]*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	roots := []*yaml.Node{}
	for count := 0; ; count++ {
		if count >= maxYAMLDocuments {
			return nil, errors.New("too many YAML documents")
		}
		var doc yaml.Node
		if err := decoder.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				return roots, nil
			}
			return nil, err
		}
		if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
			roots = append(roots, doc.Content[0])
		}
	}
}

// classifyYAML decides the flavor from top-level structure alone. Kubernetes
// wins whenever any document is a manifest; Ansible requires a list root.
func classifyYAML(roots []*yaml.Node) yamlFlavor {
	for _, root := range roots {
		if root.Kind == yaml.MappingNode && yamlValue(root, "apiVersion") != nil && yamlValue(root, "kind") != nil {
			return yamlKubernetes
		}
	}
	if len(roots) == 0 || roots[0].Kind != yaml.SequenceNode || len(roots[0].Content) == 0 {
		return yamlPlain
	}
	return classifyAnsible(roots[0].Content)
}

// classifyAnsible reads a list of mappings as plays when any names its
// hosts, and as tasks when every item has exactly one action and at least
// one action is a known module.
func classifyAnsible(items []*yaml.Node) yamlFlavor {
	for _, item := range items {
		if item.Kind != yaml.MappingNode {
			return yamlPlain
		}
	}
	for _, item := range items {
		if yamlValue(item, "hosts") != nil || ansibleImportedPlaybook(item) != nil {
			return yamlAnsiblePlaybook
		}
	}
	recognized := false
	for _, item := range items {
		action, ok := ansibleTaskAction(item)
		if !ok {
			return yamlPlain
		}
		recognized = recognized || action == "block" || ansibleActions[action] || ansibleFQCNRe.MatchString(action)
	}
	if recognized {
		return yamlAnsibleTasks
	}
	return yamlPlain
}

func yamlValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func ansibleImportedPlaybook(play *yaml.Node) *yaml.Node {
	if value := yamlValue(play, "import_playbook"); value != nil {
		return value
	}
	return yamlValue(play, "ansible.builtin.import_playbook")
}

// ansibleTaskAction returns the single key that is not a task keyword. A
// task has exactly one, which is what separates a task list from an
// arbitrary list of records that happens to use `name`.
func ansibleTaskAction(task *yaml.Node) (string, bool) {
	action := ""
	for i := 0; i+1 < len(task.Content); i += 2 {
		key := task.Content[i].Value
		if key == "block" {
			return "block", true
		}
		if ansibleTaskKeywords[key] || strings.HasPrefix(key, "with_") {
			continue
		}
		if action != "" {
			return "", false
		}
		action = key
	}
	return action, action != ""
}

// ansibleParser claims playbooks and task lists by content; every other YAML
// file falls through to the generic whole-file parser.
type ansibleParser struct{}

func (ansibleParser) Kind() string { return "ansible" }

func (ansibleParser) Match(path string, peek []byte) bool {
	if !isYAMLPath(path) {
		return false
	}
	// A full peek was probably cut mid-line; a partial last line is the
	// commonest way a truncated prefix stops being valid YAML.
	if len(peek) >= parserPeekBytes {
		if i := bytes.LastIndexByte(peek, '\n'); i >= 0 {
			peek = peek[:i+1]
		}
	}
	roots, err := yamlRoots(peek)
	if err != nil {
		return false
	}
	flavor := classifyYAML(roots)
	return flavor == yamlAnsiblePlaybook || flavor == yamlAnsibleTasks
}

func (ansibleParser) Units(path string, data []byte) []Unit { return ansibleUnits(path, data) }

// ansibleUnits emits one unit per play of a playbook or per task of a task
// list. Structure only: nothing is templated, resolved or executed, and
// malformed YAML yields no units so the caller falls back to the whole file.
func ansibleUnits(path string, data []byte) []Unit {
	roots, err := yamlRoots(data)
	if err != nil {
		return nil
	}
	flavor := classifyYAML(roots)
	if flavor != yamlAnsiblePlaybook && flavor != yamlAnsibleTasks {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	units := []Unit{}
	for r, root := range roots {
		if root.Kind != yaml.SequenceNode {
			continue
		}
		limit := len(lines)
		if r+1 < len(roots) {
			limit = roots[r+1].Line - 1
		}
		for i, item := range root.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			end := limit
			if i+1 < len(root.Content) {
				end = root.Content[i+1].Line - 1
			}
			end = yamlContentEnd(lines, item.Line, end)
			kind, name := "task", ansibleTaskName(item, i)
			if flavor == yamlAnsiblePlaybook {
				kind, name = "play", ansiblePlayName(item, i)
			}
			units = append(units, Unit{ID: path + ":" + name, Kind: kind, Name: name, Start: item.Line, End: end})
		}
	}
	return uniqueUnits(units)
}

// yamlContentEnd walks back from end over blank lines, comments and document
// markers, which belong to whatever follows rather than to this item.
func yamlContentEnd(lines []string, start, end int) int {
	for end > start {
		line := strings.TrimSpace(lines[end-1])
		if line != "" && line != "---" && line != "..." && !strings.HasPrefix(line, "#") {
			break
		}
		end--
	}
	return end
}

func ansiblePlayName(play *yaml.Node, index int) string {
	for _, value := range []*yaml.Node{yamlValue(play, "name"), yamlValue(play, "hosts"), ansibleImportedPlaybook(play)} {
		if label := yamlLabel(value); label != "" {
			return "play " + label
		}
	}
	return "play #" + strconv.Itoa(index+1)
}

func ansibleTaskName(task *yaml.Node, index int) string {
	if label := yamlLabel(yamlValue(task, "name")); label != "" {
		return "task " + label
	}
	if action, ok := ansibleTaskAction(task); ok {
		return "task " + action
	}
	return "task #" + strconv.Itoa(index+1)
}

// yamlLabel returns a scalar as a single display line.
func yamlLabel(value *yaml.Node) string {
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.Join(strings.Fields(value.Value), " ")
}
