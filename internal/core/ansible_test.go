package core

import (
	"strings"
	"testing"
)

const ansiblePlaybook = `---
# Site playbook
- name: Configure web servers
  hosts: web
  become: true
  roles:
    - common
  tasks:
    - name: Install nginx
      ansible.builtin.apt:
        name: nginx

# Database tier
- hosts: db
  tasks:
    - debug:
        msg: "{{ inventory_hostname }}"

- import_playbook: monitoring.yml
`

const ansibleTasks = `---
- name: Install packages
  apt:
    name: "{{ item }}"
  loop: [nginx, git]

- copy:
    src: app.conf
    dest: /etc/app.conf
  notify: restart app

# Grouped steps
- block:
    - name: Unpack
      community.general.archive:
        path: /tmp/a
  rescue:
    - debug: msg=failed
  when: deploy | bool

- name: Run the in-house module
  acme_deploy:
    target: prod
`

const kubernetesManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
---
apiVersion: v1
kind: Service
metadata:
  name: web
`

func scanEntry(t *testing.T, root, path string) *FileEntry {
	t.Helper()
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := idx.Entry(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return entry
}

func assertUnits(t *testing.T, got []Unit, want []Unit) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d units, got %#v", len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Kind != w.Kind || g.Name != w.Name || g.Start != w.Start || g.End != w.End {
			t.Errorf("unit %d: got %s %q %d-%d, want %s %q %d-%d", i, g.Kind, g.Name, g.Start, g.End, w.Kind, w.Name, w.Start, w.End)
		}
	}
}

func TestAnsiblePlaybookHasOneUnitPerPlay(t *testing.T) {
	root := t.TempDir()
	put(t, root, "site.yml", ansiblePlaybook)
	entry := scanEntry(t, root, "site.yml")
	if entry.Kind != "ansible" {
		t.Fatalf("expected ansible, got %q", entry.Kind)
	}
	assertUnits(t, entry.Units, []Unit{
		{Kind: "play", Name: "play Configure web servers", Start: 3, End: 11},
		{Kind: "play", Name: "play db", Start: 14, End: 17},
		{Kind: "play", Name: "play monitoring.yml", Start: 19, End: 19},
	})
}

func TestAnsibleTaskListHasOneUnitPerTask(t *testing.T) {
	root := t.TempDir()
	put(t, root, "roles/app/tasks/main.yml", ansibleTasks)
	entry := scanEntry(t, root, "roles/app/tasks/main.yml")
	if entry.Kind != "ansible" {
		t.Fatalf("expected ansible, got %q", entry.Kind)
	}
	assertUnits(t, entry.Units, []Unit{
		{Kind: "task", Name: "task Install packages", Start: 2, End: 5},
		{Kind: "task", Name: "task copy", Start: 7, End: 10},
		{Kind: "task", Name: "task block", Start: 13, End: 19},
		{Kind: "task", Name: "task Run the in-house module", Start: 21, End: 23},
	})
}

func TestPlainYAMLIsNotAnsible(t *testing.T) {
	root := t.TempDir()
	plain := map[string]string{
		"ci.yml":           "name: CI\non: [push]\njobs:\n  test:\n    steps:\n      - name: Test\n        run: go test ./...\n",
		"records.yaml":     "- name: build\n  command: make\n  dir: /src\n- name: lint\n  command: make lint\n  dir: /src\n",
		"hooks.yaml":       "- id: fmt\n  name: Format\n  entry: gofmt\n",
		"people.yml":       "- name: Ada\n  role: admin\n- name: Lin\n  role: user\n",
		"requirements.yml": "- name: geerlingguy.nginx\n  version: 3.1.0\n",
		"scalars.yml":      "- one\n- two\n",
		"empty.yml":        "",
	}
	for path, content := range plain {
		put(t, root, path, content)
	}
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	for path := range plain {
		entry, err := idx.Entry(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if entry.Kind != "yaml" || len(entry.Units) != 1 || entry.Units[0].Kind != "file" {
			t.Errorf("%s: expected a whole-file yaml entry, got %#v", path, entry)
		}
	}
}

func TestAnsibleAndKubernetesClassifiedSeparately(t *testing.T) {
	root := t.TempDir()
	put(t, root, "deploy/site.yml", ansiblePlaybook)
	put(t, root, "deploy/roles/app/tasks/main.yml", ansibleTasks)
	put(t, root, "k8s/web.yaml", kubernetesManifest)
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, file := range idx.Files {
		kinds[file.Path] = file.Kind
	}
	want := map[string]string{"deploy/site.yml": "ansible", "deploy/roles/app/tasks/main.yml": "ansible", "k8s/web.yaml": "yaml"}
	for path, kind := range want {
		if kinds[path] != kind {
			t.Errorf("%s: got kind %q, want %q", path, kinds[path], kind)
		}
	}
	for path, content := range map[string]string{"deploy/site.yml": ansiblePlaybook, "k8s/web.yaml": kubernetesManifest} {
		roots, err := yamlRoots([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
		flavor := classifyYAML(roots)
		if (path == "k8s/web.yaml") != (flavor == yamlKubernetes) {
			t.Errorf("%s: unexpected flavor %d", path, flavor)
		}
	}
	// A list-shaped file is still Kubernetes once any document is a manifest.
	roots, err := yamlRoots([]byte("- hosts: all\n---\n" + kubernetesManifest))
	if err != nil {
		t.Fatal(err)
	}
	if classifyYAML(roots) != yamlKubernetes {
		t.Error("a manifest document must win classification")
	}
}

func TestMalformedYAMLFallsBackToWholeFile(t *testing.T) {
	root := t.TempDir()
	put(t, root, "broken.yml", "- hosts: all\n  tasks:\n   - name: \"unterminated\n  bad: [1, 2\n")
	put(t, root, "ok.yml", ansiblePlaybook)
	entry := scanEntry(t, root, "broken.yml")
	if entry.Kind != "yaml" || len(entry.Units) != 1 || entry.Units[0].Kind != "file" {
		t.Fatalf("expected whole-file fallback, got %#v", entry)
	}
	if units := ansibleUnits("broken.yml", []byte("- hosts: [all\n")); units != nil {
		t.Fatalf("malformed YAML must yield no units, got %#v", units)
	}
	if len(scanEntry(t, root, "ok.yml").Units) != 3 {
		t.Fatal("a malformed neighbour must not affect other files")
	}
}

func TestAnsibleLargePlaybookClassifiedFromTruncatedPeek(t *testing.T) {
	var b strings.Builder
	b.WriteString("- name: Big play\n  hosts: all\n  tasks:\n")
	for b.Len() < 3*parserPeekBytes {
		b.WriteString("    - name: \"A long quoted task name that the peek boundary can split\"\n      ansible.builtin.debug:\n        msg: hello\n")
	}
	b.WriteString("- name: Second play\n  hosts: db\n")
	root := t.TempDir()
	put(t, root, "big.yml", b.String())
	entry := scanEntry(t, root, "big.yml")
	if entry.Kind != "ansible" || len(entry.Units) != 2 || entry.Units[1].Name != "play Second play" {
		t.Fatalf("expected two plays from a file larger than the peek, got kind %q with %d units", entry.Kind, len(entry.Units))
	}
}

func TestAnsibleUnnamedAndDuplicateUnitsStayDistinct(t *testing.T) {
	units := ansibleUnits("t.yml", []byte("- debug:\n    msg: a\n- debug:\n    msg: b\n- name: [not, a, scalar]\n  ping:\n"))
	if len(units) != 3 {
		t.Fatalf("expected 3 tasks, got %#v", units)
	}
	seen := map[string]bool{}
	for _, u := range units {
		if seen[u.ID] {
			t.Errorf("duplicate unit ID %q", u.ID)
		}
		seen[u.ID] = true
	}
	if units[2].Name != "task ping" {
		t.Errorf("a non-scalar name should fall back to the module, got %q", units[2].Name)
	}
}

func TestVaultEncryptedYAMLIsExcluded(t *testing.T) {
	root := t.TempDir()
	put(t, root, "group_vars/all/vault.yml", "$ANSIBLE_VAULT;1.1;AES256\n6338356337\n")
	put(t, root, "group_vars/all/vars.yml", "db_user: app\ndb_password: !vault |\n  $ANSIBLE_VAULT;1.1;AES256\n  6338356337\n")
	put(t, root, "notes.md", "Files start with `$ANSIBLE_VAULT;` when encrypted.\n\n    $ANSIBLE_VAULT;1.1;AES256\n")
	put(t, root, "site.yml", ansiblePlaybook)
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"group_vars/all/vault.yml", "group_vars/all/vars.yml"} {
		if _, err := idx.Entry(path); err == nil {
			t.Errorf("%s holds vault ciphertext and must not be indexed", path)
		}
	}
	for _, path := range []string{"notes.md", "site.yml"} {
		if _, err := idx.Entry(path); err != nil {
			t.Errorf("%s should be indexed: %v", path, err)
		}
	}
}
