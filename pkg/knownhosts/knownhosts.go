package knownhosts

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"

	"golang.org/x/crypto/ssh"
)

type KnownHosts []*Line

func Unmarshal(in io.Reader) (*KnownHosts, error) {
	var k KnownHosts
	s := bufio.NewScanner(in)
	var errs []string
	i := 0
	for s.Scan() {
		l, err := parseHosts(s.Text())
		if err == nil {
			if l != nil {
				k = append(k, l)
			}
		} else {
			errs = append(errs, fmt.Sprintf("%d: %s", i, err.Error()))
		}
		i++
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("knownhosts: The following lines failed to parse: %s", strings.Join(errs, ", "))
	}
	return &k, nil
}

func (k KnownHosts) Marshal(out io.Writer) error {
	for _, l := range k {
		b, err := l.Marshal()
		if err != nil {
			return err
		}
		if _, err := out.Write(b); err != nil {
			return err
		}
	}
	return nil
}

var HostKeyMismatchError = errors.New("knownhosts: Host key doesn't match")
var HostNotFoundError = errors.New("knownhosts: Host is unknown")
var HostRevokedError = errors.New("knownhosts: Host is revoked")
var UnsupportedAddrType = errors.New("knownhosts: remote must be a *net.TCPAddr")

func (k KnownHosts) HostKeyCallback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	var addr *net.TCPAddr
	if v, ok := remote.(*net.TCPAddr); ok {
		addr = v
	} else {
		return UnsupportedAddrType
	}
	keyBytes := key.Marshal()
	var matched []*Host
	for _, l := range k {
		if l.CertAuthority {
			continue
		}
		if key.Type() != l.PublicKey.Type() {
			continue
		}
		lKeyBytes := l.PublicKey.Marshal()
		for _, h := range l.Hosts {
			if h.Match(hostname, addr) {
				if !bytes.Equal(keyBytes, lKeyBytes) {
					return HostKeyMismatchError
				}
				if l.Revoked {
					return HostRevokedError
				}
				matched = append(matched, h)
			}
		}
	}
	if len(matched) == 0 {
		return HostNotFoundError
	}
	return nil
}

func (k *KnownHosts) AppendHost(hostname string, key ssh.PublicKey, out io.Writer) error {
	hostname, port, err := net.SplitHostPort(hostname)
	if err != nil {
		return err
	}
	l := &Line{
		PublicKey: key,
	}
	h := &Host{
		Addr: hostname,
		Port: port,
		line: l,
	}
	l.Hosts = []*Host{h}
	*k = append(*k, l)
	if out != nil {
		data, err := l.Marshal()
		if err != nil {
			return err
		}
		if _, err := out.Write(data); err != nil {
			return err
		}
	}
	return nil
}

type Line struct {
	Hosts         []*Host
	Revoked       bool
	CertAuthority bool
	PublicKey     ssh.PublicKey
}

func (l *Line) Marshal() ([]byte, error) {
	const sep = " "
	var buf bytes.Buffer

	// flag part
	var flag string
	if l.Revoked {
		flag = "revoked"
	}
	if l.CertAuthority {
		flag = "cert-authority"
	}
	if flag != "" {
		fmt.Fprintf(&buf, "@%s", flag)
		buf.WriteString(sep)
	}

	// hosts part
	for i, h := range l.Hosts {
		if h.Hash != nil && (i != 0 || len(l.Hosts) > 1) {
			return nil, fmt.Errorf("knownhosts: Hashed host must be only host on line, found %d!", len(l.Hosts))
		}
		if b := h.Marshal(); b != nil {
			buf.Write(b)
		} else {
			return nil, fmt.Errorf("knownhosts: Invalid host: %#v", h)
		}
		if i != len(l.Hosts)-1 {
			buf.WriteString(",")
		}
	}
	buf.WriteString(sep)

	// key part (has trailing newline)
	buf.Write(ssh.MarshalAuthorizedKey(l.PublicKey))

	return buf.Bytes(), nil
}

type Host struct {
	Hash           *hostHash
	Pattern        *regexp.Regexp
	PatternNegated bool
	PatternRaw     string
	Addr           string
	Port           string

	line *Line
}

func (h *Host) Match(hostname string, addr *net.TCPAddr) bool {
	hostname, port, err := net.SplitHostPort(hostname)
	if err != nil {
		return false
	}
	if h.Hash != nil {
		return h.Hash.Match(hostname, port)
	}
	if port != h.Port {
		return false
	}
	if h.Pattern != nil {
		m := h.Pattern.MatchString(hostname)
		if m {
			return !h.PatternNegated
		}
		return h.PatternNegated
	}
	return h.Addr == hostname
}

func (h *Host) Marshal() []byte {
	if h.Hash != nil {
		return []byte(h.Hash.Raw)
	} else if h.Pattern != nil {
		return []byte(h.PatternRaw)
	} else if h.Port != "22" {
		return []byte(fmt.Sprintf("[%s]:%s", h.Addr, h.Port))
	}
	return []byte(h.Addr)
}

type hostHash struct {
	Raw  string
	Salt []byte
	Sum  []byte
}

func (hh *hostHash) Match(hostname string, port string) bool {
	if port != "22" {
		hostname = fmt.Sprintf("[%s]:%s", hostname, port)
	}
	mac := hmac.New(sha1.New, hh.Salt)
	mac.Write([]byte(hostname))
	sum := mac.Sum(nil)
	return hmac.Equal(sum, hh.Sum)
}

func parseHosts(line string) (*Line, error) {
	const whitespace = "\t "

	l := &Line{
		Revoked:       false,
		CertAuthority: false,
	}

	line = strings.TrimLeft(line, whitespace)
	line = strings.TrimRight(line, whitespace)

	nextPart := func(line string) (string, string, error) {
		i := strings.IndexAny(line, whitespace)
		if i < 0 {
			return "", "", fmt.Errorf("knownhosts: Invalid line: %s", line)
		}
		return line[0:i], strings.TrimLeft(line[i:], whitespace), nil
	}

	// ignore empty lines and comments
	if line == "" || line[0] == '#' {
		return nil, nil
	}

	// parse flags
	if line[0] == '@' {
		var flag string
		var err error
		flag, line, err = nextPart(line)
		if err != nil {
			return nil, err
		}
		flag = flag[1:] // trim @

		switch flag {
		case "revoked":
			l.Revoked = true
		case "cert-authority":
			l.CertAuthority = true
		default:
			return nil, fmt.Errorf("knownhosts: Unknown flag @%s", flag)
		}
	}

	// parse hosts
	{
		var part string
		var err error
		part, line, err = nextPart(line)
		if err != nil {
			return nil, err
		}
		if part != "" && part[0] == '|' {
			hash, err := parseHostHash(part)
			if err != nil {
				return nil, fmt.Errorf("knownhosts: Error parsing hashed host: %#v: %s", part, err)
			}
			l.Hosts = append(l.Hosts, &Host{
				Hash: hash,
				Port: "22",
				line: l,
			})
		} else {
			for _, h := range strings.Split(part, ",") {
				host, err := parseHost(h)
				if err != nil {
					return nil, fmt.Errorf("knownhosts: Error parsing host: %#v: %s", h, err)
				} else {
					host.line = l
					l.Hosts = append(l.Hosts, host)
				}
			}
		}
	}

	// the remainder is the key
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil, err
	}
	l.PublicKey = key

	return l, nil
}

func parseHost(str string) (*Host, error) {
	host := &Host{
		Addr: str,
		Port: "22",
	}
	if str != "" && str[0] == '[' {
		i := strings.LastIndex(str, ":")
		if i == -1 || i-1 < 1 {
			return nil, fmt.Errorf("knownhosts: Invalid host: %#v", str)
		}
		host.Addr = str[1 : i-1]
		host.Port = str[i+1:]
	} else if strings.ContainsAny(str, "*?") {
		pattern := str
		if pattern[0] == '!' {
			pattern = pattern[1:]
			host.PatternNegated = true
		}
		pattern = strings.Replace(pattern, ".", "\\.", -1)
		pattern = strings.Replace(pattern, "*", ".*", -1)
		pattern = strings.Replace(pattern, "?", ".", -1)
		r, err := regexp.Compile("^" + pattern + "$")
		if err != nil {
			return nil, err
		}
		host.Addr = ""
		host.Pattern = r
		host.PatternRaw = str
	}
	return host, nil
}

func parseHostHash(str string) (*hostHash, error) {
	hh := &hostHash{
		Raw: str,
	}
	parts := strings.Split(str, "|")[1:]
	if len(parts) != 3 || parts[0] != "1" {
		return nil, fmt.Errorf("knownhosts: Invalid hash: %s", str)
	}
	var err error
	hh.Salt, err = base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	hh.Sum, err = base64.StdEncoding.DecodeString(parts[2])
	return hh, err
}
