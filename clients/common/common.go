package common

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"gopkg.in/src-d/go-git.v2/core"
	"gopkg.in/src-d/go-git.v2/formats/pktline"

	"gopkg.in/sourcegraph/go-vcsurl.v1"
)

var (
	NotFoundErr           = errors.New("repository not found")
	EmptyGitUploadPackErr = errors.New("empty git-upload-pack given")
)

const GitUploadPackServiceName = "git-upload-pack"

type Endpoint string

func NewEndpoint(url string) (Endpoint, error) {
	vcs, err := vcsurl.Parse(url)
	if err != nil {
		return "", core.NewPermanentError(err)
	}

	link := vcs.Link()
	if !strings.HasSuffix(link, ".git") {
		link += ".git"
	}

	return Endpoint(link), nil
}

func (e Endpoint) Service(name string) string {
	return fmt.Sprintf("%s/info/refs?service=%s", e, name)
}

// Capabilities contains all the server capabilities
// https://github.com/git/git/blob/master/Documentation/technical/protocol-capabilities.txt
type Capabilities struct {
	m map[string]*Capability
	o []string
}

// Capability represents a server capability
type Capability struct {
	Name   string
	Values []string
}

// NewCapabilities returns a new Capabilities struct
func NewCapabilities() *Capabilities {
	return &Capabilities{
		m: make(map[string]*Capability, 0),
	}
}

// Decode decodes a string
func (c *Capabilities) Decode(raw string) {
	parts := strings.SplitN(raw, "HEAD", 2)
	if len(parts) == 2 {
		raw = parts[1]
	}

	params := strings.Split(raw, " ")
	for _, p := range params {
		s := strings.SplitN(p, "=", 2)

		var value string
		if len(s) == 2 {
			value = s[1]
		}

		c.Add(s[0], value)
	}
}

// Add adds a capability, values are optional
func (c *Capabilities) Add(capability string, values ...string) {
	if !c.Supports(capability) {
		c.m[capability] = &Capability{Name: capability}
		c.o = append(c.o, capability)
	}

	if len(values) == 0 {
		return
	}

	c.m[capability].Values = append(c.m[capability].Values, values...)
}

// Supports returns true if capability is present
func (c *Capabilities) Supports(capability string) bool {
	_, ok := c.m[capability]
	return ok
}

// Get returns the values for a capability
func (c *Capabilities) Get(capability string) *Capability {
	return c.m[capability]
}

// SymbolicReference returns the reference for a given symbolic reference
func (c *Capabilities) SymbolicReference(sym string) string {
	if !c.Supports("symref") {
		return ""
	}

	for _, symref := range c.Get("symref").Values {
		parts := strings.Split(symref, ":")
		if len(parts) != 2 {
			continue
		}

		if parts[0] == sym {
			return parts[1]
		}
	}

	return ""
}

func (c *Capabilities) String() string {
	if len(c.o) == 0 {
		return ""
	}

	var o string
	for _, key := range c.o {
		cap := c.m[key]
		if len(cap.Values) == 0 {
			o += key + " "
		}

		for _, value := range cap.Values {
			o += fmt.Sprintf("%s=%s ", key, value)
		}
	}

	return o[:len(o)-1]
}

type GitUploadPackInfo struct {
	Capabilities *Capabilities
	Head         string
	Refs         map[string]core.Hash
}

func NewGitUploadPackInfo() *GitUploadPackInfo {
	return &GitUploadPackInfo{Capabilities: NewCapabilities()}
}

func (r *GitUploadPackInfo) Decode(d *pktline.Decoder) error {
	if err := r.read(d); err != nil {
		if err == EmptyGitUploadPackErr {
			return core.NewPermanentError(err)
		}

		return core.NewUnexpectedError(err)
	}

	return nil
}

func (r *GitUploadPackInfo) read(d *pktline.Decoder) error {
	lines, err := d.ReadAll()
	if err != nil {
		return err
	}

	isEmpty := true
	r.Refs = map[string]core.Hash{}
	for _, line := range lines {
		if !r.isValidLine(line) {
			continue
		}

		if len(r.Capabilities.o) == 0 {
			r.Capabilities.Decode(line)
			continue
		}

		r.readLine(line)
		isEmpty = false
	}

	if isEmpty {
		return EmptyGitUploadPackErr
	}

	return nil
}

func (r *GitUploadPackInfo) isValidLine(line string) bool {
	if line[0] == '#' {
		return false
	}

	return true
}

func (r *GitUploadPackInfo) readLine(line string) {
	parts := strings.Split(strings.Trim(line, " \n"), " ")
	if len(parts) != 2 {
		return
	}

	r.Refs[parts[1]] = core.NewHash(parts[0])
}

func (r *GitUploadPackInfo) String() string {
	return string(r.Bytes())
}

func (r *GitUploadPackInfo) Bytes() []byte {
	e := pktline.NewEncoder()
	e.AddLine("# service=git-upload-pack")
	e.AddFlush()
	e.AddLine(fmt.Sprintf("%s HEAD\x00%s", r.Refs[r.Head].String(), r.Capabilities.String()))

	for name, id := range r.Refs {
		e.AddLine(fmt.Sprintf("%s %s", id, name))
	}

	e.AddFlush()
	b, _ := ioutil.ReadAll(e.Reader())
	return b
}

type GitUploadPackRequest struct {
	Want []core.Hash
	Have []core.Hash
}

func (r *GitUploadPackRequest) String() string {
	b, _ := ioutil.ReadAll(r.Reader())
	return string(b)
}

func (r *GitUploadPackRequest) Reader() *strings.Reader {
	e := pktline.NewEncoder()
	for _, want := range r.Want {
		e.AddLine(fmt.Sprintf("want %s", want))
	}

	for _, have := range r.Have {
		e.AddLine(fmt.Sprintf("have %s", have))
	}

	e.AddFlush()
	e.AddLine("done")

	return e.Reader()
}
