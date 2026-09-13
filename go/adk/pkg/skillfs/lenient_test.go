package skillfs

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"google.golang.org/adk/v2/tool/skilltoolset/skill"
)

const body = "\n# Deploy\n\nRun the deploy script.\n"

func skillMD(frontmatter string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("---\n" + frontmatter + "---\n" + body)}
}

func TestLenientLoadsSkillsWithFieldsOutsideTheSpec(t *testing.T) {
	fsys := Lenient(fstest.MapFS{
		"deploy/SKILL.md":       skillMD("name: deploy\ndescription: Deploys the service.\nuser-invocable: false\nargument-hint: '[environment]'\nversion: 0.1.0\nmetadata:\n  author: platform\n"),
		"review/SKILL.md":       skillMD("name: review\ndescription: Reviews a change.\nallowed-tools:\n  - Read\n"),
		"deploy/scripts/run.sh": {Data: []byte("#!/bin/sh\n")},
		"README.md":             {Data: []byte("not a skill\n")},
	})
	source := skill.NewFileSystemSource(fsys)

	frontmatters, err := source.ListFrontmatters(context.Background())
	if err != nil {
		t.Fatalf("ListFrontmatters: %v", err)
	}
	if len(frontmatters) != 2 {
		t.Fatalf("got %d skills, want 2", len(frontmatters))
	}
	deploy, err := source.LoadFrontmatter(context.Background(), "deploy")
	if err != nil {
		t.Fatalf("LoadFrontmatter(deploy): %v", err)
	}
	if deploy.Name != "deploy" || deploy.Description != "Deploys the service." || deploy.Metadata["author"] != "platform" {
		t.Errorf("known fields lost: %+v", deploy)
	}
	instructions, err := source.LoadInstructions(context.Background(), "deploy")
	if err != nil {
		t.Fatalf("LoadInstructions(deploy): %v", err)
	}
	if instructions != body {
		t.Errorf("instructions = %q, want %q", instructions, body)
	}
	resources, err := source.ListResources(context.Background(), "deploy", "")
	if err != nil || len(resources) != 1 || resources[0] != "scripts/run.sh" {
		t.Errorf("ListResources = %v, %v", resources, err)
	}
}

func TestLenientKeepsTheADKsValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		files   fstest.MapFS
		wantErr error
	}{
		"missing description": {
			files:   fstest.MapFS{"deploy/SKILL.md": skillMD("name: deploy\nuser-invocable: false\n")},
			wantErr: skill.ErrInvalidFrontmatter,
		},
		"only unknown fields": {
			files:   fstest.MapFS{"deploy/SKILL.md": skillMD("user-invocable: false\nversion: 1\n")},
			wantErr: skill.ErrInvalidFrontmatter,
		},
		"name does not match the directory": {
			files:   fstest.MapFS{"deploy/SKILL.md": skillMD("name: ship\ndescription: Ships.\nversion: 1\n")},
			wantErr: skill.ErrInvalidSkillName,
		},
		"no closing separator": {
			files:   fstest.MapFS{"deploy/SKILL.md": {Data: []byte("---\nname: deploy\ndescription: Deploys.\nversion: 1\n")}},
			wantErr: skill.ErrInvalidFrontmatter,
		},
		"frontmatter is not a mapping": {
			files:   fstest.MapFS{"deploy/SKILL.md": skillMD("- name: deploy\n")},
			wantErr: skill.ErrInvalidFrontmatter,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := skill.NewFileSystemSource(Lenient(tc.files)).ListFrontmatters(context.Background())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDropUnknownFields(t *testing.T) {
	for name, tc := range map[string]struct {
		in, want string
	}{
		"unknown fields are dropped, the rest is kept in order": {
			in:   "---\nname: deploy\nuser-invocable: false\ndescription: Deploys.\nversion: 0.1.0\n---\nbody\n",
			want: "---\nname: deploy\ndescription: Deploys.\n---\nbody\n",
		},
		"nothing to drop is returned as is": {
			in:   "---\nname: deploy\ndescription: Deploys.\n---\nbody\n",
			want: "---\nname: deploy\ndescription: Deploys.\n---\nbody\n",
		},
		"windows line endings": {
			in:   "---\r\nname: deploy\r\ndescription: Deploys.\r\nversion: 1\r\n---\r\nbody\r\n",
			want: "---\r\nname: deploy\ndescription: Deploys.\n---\r\nbody\r\n",
		},
		"the body keeps its own separators": {
			in:   "---\nname: deploy\ndescription: Deploys.\nversion: 1\n---\n\n---\nnot frontmatter\n",
			want: "---\nname: deploy\ndescription: Deploys.\n---\n\n---\nnot frontmatter\n",
		},
		"nested unknown keys under metadata are kept": {
			in:   "---\nname: deploy\ndescription: Deploys.\nmetadata:\n  version: 0.1.0\nversion: 1\n---\n",
			want: "---\nname: deploy\ndescription: Deploys.\nmetadata:\n  version: 0.1.0\n---\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DropUnknownFields([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestDropUnknownFieldsRefusesWhatIsNotFrontmatter(t *testing.T) {
	for name, in := range map[string]string{
		"no opening separator":     "name: deploy\n---\n",
		"no closing separator":     "---\nname: deploy\n",
		"not a mapping":            "---\n- deploy\n---\n",
		"yaml that does not parse": "---\nname: [\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DropUnknownFields([]byte(in)); !errors.Is(err, errNotFrontmatter) {
				t.Fatalf("err = %v, want %v", err, errNotFrontmatter)
			}
		})
	}
}

func TestLenientServesOtherFilesUnchanged(t *testing.T) {
	fsys := Lenient(fstest.MapFS{
		"deploy/SKILL.md":            skillMD("name: deploy\ndescription: Deploys.\nversion: 1\n"),
		"deploy/references/SKILL.md": {Data: []byte("---\nversion: 1\n---\n")},
		"deploy/references/notes.md": {Data: []byte("---\nversion: 1\n---\n")},
	})
	for _, name := range []string{"deploy/references/SKILL.md", "deploy/references/notes.md"} {
		got, err := fs.ReadFile(fsys, name)
		if err != nil || string(got) != "---\nversion: 1\n---\n" {
			t.Errorf("%s = %q, %v; want unchanged", name, got, err)
		}
	}
	got, err := fs.ReadFile(fsys, "deploy/SKILL.md")
	if err != nil || strings.Contains(string(got), "version") {
		t.Errorf("deploy/SKILL.md = %q, %v; want the version field dropped", got, err)
	}
	info, err := fs.Stat(fsys, "deploy/SKILL.md")
	if err != nil || info.Size() != int64(len(got)) {
		t.Errorf("Stat size = %d, %v; want %d", info.Size(), err, len(got))
	}
}
