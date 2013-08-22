package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"code.google.com/p/go9p/p"
	"code.google.com/p/go9p/p/srv"
	"io/ioutil"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Implements p.User and p.Group
type User struct {
	n string
}

func (u *User) Name() string            { return u.n }
func (u *User) Id() int                 { return -1 }
func (u *User) Groups() []p.Group       { return []p.Group{u} }
func (u *User) IsMember(g p.Group) bool { return false }
func (u *User) Members() []p.User       { return []p.User{u} }

type HttpClient struct {
	dir *srv.File
	baseurl *url.URL
	url *url.URL
	req  *http.Request
	resp *http.Response
	body []byte
	id   int
}

type ClientBody struct {
	c *HttpClient
	srv.File
}

// Kickoff request
func (cbody *ClientBody) Open(fid *srv.FFid, mode uint8) error {
	var err error

	if cbody.c.url == nil {
		return fmt.Errorf("body: %d url not set", cbody.c.id)
	}

	if cbody.c.baseurl != nil {
		cbody.c.req.URL = cbody.c.baseurl.ResolveReference(cbody.c.url)
	} else {
		cbody.c.req.URL = cbody.c.url
	}

	if *debug > 0 {
		log.Printf("body: launching request %+v", cbody.c.req)
	}

	cbody.c.req.Host = cbody.c.req.URL.Host

	cbody.c.resp, err = http.DefaultClient.Do(cbody.c.req)

	if *debug > 0 {
		log.Printf("body: error %q response %+v", err, cbody.c.resp)
	}

	if err != nil {
		return fmt.Errorf("body: req.Do: %s", err)
	}

	defer cbody.c.resp.Body.Close()

	cbody.c.body, err = ioutil.ReadAll(cbody.c.resp.Body)

	if err != nil {
		return fmt.Errorf("body: read: %s", err)
	}

	for hname, values := range cbody.c.resp.Header {
		trim := strings.ToLower(strings.Replace(hname, "-", "", -1))
		hfile := &HeaderFile{key: trim, value: strings.Join(values, " ")}
		if p9err := hfile.Add(cbody.c.dir, trim, user, user, 0444, hfile); p9err != nil {
			return fmt.Errorf("body: can't make header file: %s", p9err)
		}
	}

	return nil
}

func (cbody *ClientBody) Read(fid *srv.FFid, buf []byte, offset uint64) (int, error) {
	if cbody.c.url == nil {
		return 0, fmt.Errorf("body: %d url not set", cbody.c.id)
	}

	b := cbody.c.body
	n := len(b)
	if offset >= uint64(n) {
		return 0, nil
	}

	b = b[int(offset):n]
	n -= int(offset)
	if len(buf) < n {
		n = len(buf)
	}

	copy(buf[:], b[:])
	return n, nil
}

type ClientCtl struct {
	c *HttpClient
	srv.File
}

func (cctl *ClientCtl) Read(fid *srv.FFid, buf []byte, offset uint64) (int, error) {
	// we only allow a single read from us, change the offset and we're done
	if offset > uint64(0) {
		return 0, nil
	}

	id := fmt.Sprintf("%d\n", cctl.c.id)

	b := []byte(id)
	if len(buf) < len(b) {
		return 0, &p.Error{"not enough buffer space for result", 0}
	}

	copy(buf, b)
	return len(b), nil
}

func (cctl *ClientCtl) Write(fid *srv.FFid, data []byte, offset uint64) (int, error) {
	sc := bufio.NewScanner(bytes.NewBuffer(data))
	for sc.Scan() {
		l := sc.Text()
		log.Printf("ctl: %d %s", cctl.c.id, l)
		fields := strings.Fields(l)

		switch fields[0] {
		case "url":
			if len(fields) < 2 {
				return 0, fmt.Errorf("ctl: missing argument in control message")
			}

			url, err := url.Parse(fields[1])
			if err != nil {
				return 0, fmt.Errorf("ctl: url parse: %s", err)
			}
			cctl.c.url = url
		case "baseurl":
			if len(fields) < 2 {
				return 0, fmt.Errorf("ctl: missing argument in control message")
			}

			url, err := url.Parse(fields[1])
			if err != nil {
				return 0, fmt.Errorf("ctl: url parse: %s", err)
			}
			cctl.c.baseurl = url
		default:
			return 0, fmt.Errorf("ctl: unknown control message %s", fields[0])
		}
	}

	return len(data), nil
}

type Qparsed int

const (
	Qurl Qparsed = iota
	Qscheme
	Quser
	Qpass
	Qhost
	Qport
	Qpath
	Qquery
	Qfragment
)

var (
	parsedtab = map[Qparsed]string {
		Qurl: "url",
		Qscheme: "scheme",
		Quser: "user",
		Qpass: "pass",
		Qhost: "host",
		Qport: "port",
		Qpath: "path",
		Qquery: "query",
		Qfragment: "fragment",
	}
)

type ParsedFile struct {
	srv.File
	c *HttpClient
	ptype Qparsed
}

func (pf *ParsedFile) Read(fid *srv.FFid, buf []byte, offset uint64) (int, error) {
	if pf.c.req.URL == nil {
		return 0, fmt.Errorf("body: %d url not set", pf.c.id)
	}

	log.Printf("parsedfile read: %s", parsedtab[pf.ptype])

	out := new(bytes.Buffer)

	switch pf.ptype {
		case Qurl:
			io.WriteString(out, pf.c.req.URL.String())
		case Qfragment:
			io.WriteString(out, pf.c.req.URL.Fragment)
		default:
			return 0, srv.Enotimpl
	}

	copy(buf, out.Bytes())
	return out.Len(), nil
}

type HeaderFile struct {
	srv.File
	c *HttpClient

	key, value string
}

func (hf *HeaderFile) Read(fid *srv.FFid, buf []byte, offset uint64) (int, error) {
	if offset > 0 {
		return 0, nil
	}

	log.Printf("headerfile read: %s", hf.key)

	out := new(bytes.Buffer)

	io.WriteString(out, hf.value)

	copy(buf, out.Bytes())
	return out.Len(), nil
}

// Root clone
type Clone struct {
	srv.File
	clones int
}

func (cl *Clone) Read(fid *srv.FFid, buf []byte, offset uint64) (int, error) {
	var err error

	var dir *srv.File
	var ctl *ClientCtl
	var body *ClientBody
	var parsed *srv.File

	var outb []byte

	// we only allow a single read from us, change the offset and we're done
	if offset > uint64(0) {
		return 0, nil
	}

	id := fmt.Sprintf("%d", cl.clones)

	client := new(HttpClient)
	client.id = cl.clones
	client.req = &http.Request{
		Method:     "GET",
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}

	// web/0
	dir = new(srv.File)
	if p9err := dir.Add(root, id, user, user, p.DMDIR|0555, nil); p9err != nil {
		err = fmt.Errorf("clone: can't add client dir: %s", p9err)
		goto remove
	}
	client.dir = dir

	// web/0/ctl
	ctl = new(ClientCtl)
	if p9err := ctl.Add(dir, "ctl", user, user, 0666, ctl); p9err != nil {
		err = fmt.Errorf("clone: can't make ctl: %s", p9err)
		goto remove
	}
	ctl.c = client

	// web/0/body
	body = new(ClientBody)
	if p9err := body.Add(dir, "body", user, user, 0444, body); p9err != nil {
		err = fmt.Errorf("clone: can't make body: %s", p9err)
		goto remove
	}
	body.c = client

	// web/0/parsed/
	parsed = new(srv.File)
	if p9err := parsed.Add(dir, "parsed", user, user, p.DMDIR|0555, nil); p9err != nil {
		err = fmt.Errorf("clone: can't add parsed dir: %s", p9err)
		goto remove
	}

	for ptype, fname := range parsedtab {
		pfile := &ParsedFile{ptype: ptype, c: client}
		if p9err := pfile.Add(parsed, fname, user, user, 0444, pfile); p9err != nil {
			err = fmt.Errorf("clone: can't add parsed file %s: %s", p9err)
			goto remove
		}
	}

	outb = []byte(id + "\n")
	if len(buf) < len(outb) {
		err = fmt.Errorf("not enough buffer space for result")
		goto remove
	}

	fid.F = &ctl.File
	cl.clones++

	copy(buf, outb)
	return len(outb), nil

remove:
	if dir != nil {
		dir.Remove()
	}
	return 0, err
}
// Root ctl
type RootCtl struct {
	srv.File

	// flushauth
	// preauth
}

func (c *RootCtl) Read(fid *srv.FFid, buf []byte, offset uint64) (int, error) {
	config := new(bytes.Buffer)

	fmt.Fprintf(config, "useragent %s\n", useragent)
	fmt.Fprintf(config, "timeout %d\n", timeout)

	b := config.Bytes()
	clen := len(b)

	if offset >= uint64(clen) {
		return 0, nil
	}

	if clen > len(buf) {
		clen = len(buf)
	}

	r := copy(buf, b[0:clen])
	return r, nil
}

var (
	// cli flags
	addr  = flag.String("addr", ":5640", "listen address")
	debug = flag.Int("d", 0, "print debug messages")

	aflag = flag.String("A", "hjdicks", "useragent")
	tflag = flag.Int("T", 10000, "timeout")

	// global webfs settings
	useragent string
	timeout   int

	// 9p fs user/group
	user *User

	// special file handles
	root *srv.File
	cl   *Clone
	ctl  *RootCtl
)

func init() {
	un := os.Getenv("user")
	if un != "" {
		user = &User{un}
	} else {
		user = &User{"none"}
	}
}

func main() {
	var err error
	var s *srv.Fsrv
	flag.Parse()

	useragent = *aflag
	timeout = *tflag

	root = new(srv.File)

	err = root.Add(nil, "/", user, user, p.DMDIR|0666, root)
	if err != nil {
		goto error
	}

	cl = new(Clone)
	if err = cl.Add(root, "clone", user, user, 0666, cl); err != nil {
		goto error
	}

	ctl = new(RootCtl)
	if err = ctl.Add(root, "ctl", user, user, 0666, ctl); err != nil {
		goto error
	}

	s = srv.NewFileSrv(root)
	s.Debuglevel = *debug
	s.Start(s)
	s.Id = "webfs"

	err = s.StartNetListener("tcp", *addr)
	if err != nil {
		goto error
	}
	/*
		webfs := new(Webfs)
		webfs.useragent = "hjdicks"
		webfs.timeout = 10000
		webfs.Id = "webfs"
		webfs.Debuglevel = 1
		webfs.Start(webfs)
		err := webfs.StartNetListener("tcp", ":5640")
		if err != nil {
			goto error
		}*/

	return

error:
	log.Println(fmt.Sprintf("Error: %s", err))

}
