package knownhosts

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net"
	"strings"
	"testing"

	"github.com/flynn/flynn/pkg/random"
	. "github.com/flynn/go-check"
	"golang.org/x/crypto/ssh"
)

func Test(t *testing.T) {
	TestingT(t)
}

var _ = Suite(&S{})

type S struct{}

func genPublicKey(c *C) ssh.PublicKey {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	c.Assert(err, IsNil)
	var pemBuf bytes.Buffer
	pem.Encode(&pemBuf, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
	})
	rsaPubKey, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	c.Assert(err, IsNil)
	return rsaPubKey
}

func (S) TestNonPatterns(c *C) {
	const sep = " "
	var input bytes.Buffer

	key := genPublicKey(c)
	keyBytes := bytes.TrimRight(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key)), "\n")

	// format: host key
	host1Addr := "101.102.103.72"
	input.WriteString(host1Addr)
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")

	// format: host key
	host2Addr := "test.example.com"
	input.WriteString(host2Addr)
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")

	// format: @flag [host]:port key
	host3Addr := "3.example.com"
	host3Port := "2222"
	input.WriteString("@revoked")
	input.WriteString(sep)
	input.WriteString("[" + host3Addr + "]:" + host3Port)
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")

	// format: host,host,host key
	host4Addr := "4.example.com"
	host5Addr := "102.101.72.100"
	host6Addr := "6.example.com"
	input.WriteString(strings.Join([]string{host4Addr, host5Addr, host6Addr}, ","))
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")

	// format: host,[host]:port,host key
	host7Addr := "7.example.com"
	host8Addr := "102.101.72.100"
	host8Port := "2223"
	host9Addr := "9.example.com"
	input.WriteString(strings.Join([]string{host7Addr, "[" + host8Addr + "]:" + host8Port, host9Addr}, ","))
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")

	// format: @flag host,host key
	host10Addr := "10.example.com"
	host11Addr := "11.example.com"
	input.WriteString("@revoked")
	input.WriteString(sep)
	input.WriteString(strings.Join([]string{host10Addr, host11Addr}, ","))
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")

	// format: hashed
	host12Addr := "12.example.com"
	host12Salt := random.Bytes(16)
	host12SaltEncoded := base64.StdEncoding.EncodeToString(host12Salt)
	host12Mac := hmac.New(sha1.New, host12Salt)
	host12Mac.Write([]byte(host12Addr))
	host12MacEncoded := base64.StdEncoding.EncodeToString(host12Mac.Sum(nil))
	input.WriteString("|1|" + host12SaltEncoded + "|" + host12MacEncoded)
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")

	k, err := Unmarshal(bytes.NewReader(input.Bytes()))
	c.Assert(err, IsNil)

	// Test HostKeyCallback
	addr := &net.TCPAddr{
		Port: 22,
	}
	c.Assert(k.HostKeyCallback(host1Addr+":22", addr, key), IsNil)
	c.Assert(k.HostKeyCallback(host2Addr+":22", addr, key), IsNil)
	c.Assert(k.HostKeyCallback(host3Addr+":2222", &net.TCPAddr{Port: 2222}, key), Equals, HostRevokedError)
	c.Assert(k.HostKeyCallback(host4Addr+":22", addr, key), IsNil)
	c.Assert(k.HostKeyCallback(host5Addr+":22", addr, key), IsNil)
	c.Assert(k.HostKeyCallback(host6Addr+":22", addr, key), IsNil)
	c.Assert(k.HostKeyCallback(host7Addr+":22", addr, key), IsNil)
	c.Assert(k.HostKeyCallback(host8Addr+":2223", &net.TCPAddr{Port: 2223}, key), IsNil)
	c.Assert(k.HostKeyCallback(host9Addr+":22", addr, key), IsNil)
	c.Assert(k.HostKeyCallback(host10Addr+":22", addr, key), Equals, HostRevokedError)
	c.Assert(k.HostKeyCallback(host11Addr+":22", addr, key), Equals, HostRevokedError)
	c.Assert(k.HostKeyCallback("notfound.example.com:22", addr, key), Equals, HostNotFoundError)
	c.Assert(k.HostKeyCallback(host3Addr+":2223", &net.TCPAddr{Port: 2223}, key), Equals, HostNotFoundError)
	c.Assert(k.HostKeyCallback(host1Addr+":2222", &net.TCPAddr{Port: 2222}, key), Equals, HostNotFoundError)
	c.Assert(k.HostKeyCallback(host12Addr+":22", addr, key), IsNil) // hash match

	// Make sure output is the same as input
	var output bytes.Buffer
	c.Assert(k.Marshal(&output), IsNil)
	c.Assert(output.String(), Equals, input.String())

	// Test AppendHost with Writer
	var output2 bytes.Buffer
	var input2 bytes.Buffer
	c.Assert(k.AppendHost("new1.example.com:2223", key, &output2), IsNil)
	input2.WriteString("[new1.example.com]:2223")
	input2.WriteString(sep)
	input2.Write(keyBytes)
	input2.WriteString("\n")
	input.Write(input2.Bytes())
	c.Assert(output2.String(), Equals, input2.String())

	// Test AppendHost without Writer
	c.Assert(k.AppendHost("new2.example.com:22", key, nil), IsNil)
	input.WriteString("new2.example.com")
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")
	output.Reset()
	c.Assert(k.Marshal(&output), IsNil)
	c.Assert(output.String(), Equals, input.String())
}

func (S) TestPatterns(c *C) {
	const sep = " "
	var input bytes.Buffer

	key := genPublicKey(c)
	keyBytes := bytes.TrimRight(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key)), "\n")

	// format: pattern
	input.WriteString("*.example")
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")

	// format: negated pattern
	input.WriteString("!*.example.or?")
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")

	k, err := Unmarshal(bytes.NewReader(input.Bytes()))
	c.Assert(err, IsNil)

	// Test HostKeyCallback
	addr := &net.TCPAddr{
		Port: 22,
	}
	c.Assert(k.HostKeyCallback("foo.example:22", addr, key), IsNil)                         // pattern match
	c.Assert(k.HostKeyCallback("foo.example.org:22", addr, key), Equals, HostNotFoundError) // negated pattern match
	c.Assert(k.HostKeyCallback("anything.example.com:22", addr, key), IsNil)                // negated pattern miss

	// Make sure output is the same as input
	var output bytes.Buffer
	c.Assert(k.Marshal(&output), IsNil)
	c.Assert(output.String(), Equals, input.String())
}

func (S) TestMixedKeyTypes(c *C) {
	var input bytes.Buffer

	ipAddress := "192.0.2.203"

	// ECDSA
	ecdsaKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte("ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBAN1At7ODzOADlqMknviOG5GRHjVy53PPC1DVhun2pMhzCjNgHMt/XvRaeMKhRvUUaUVaNLmCBi75B/2KJH289g="))
	c.Assert(err, IsNil)
	c.Assert(ecdsaKey, NotNil)
	input.WriteString("|1|apo1+O3sutQ2AxrFoUgiucqL2vs=|fQljlEPB/ICE1TT7VMBYqQhiq+w= ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBAN1At7ODzOADlqMknviOG5GRHjVy53PPC1DVhun2pMhzCjNgHMt/XvRaeMKhRvUUaUVaNLmCBi75B/2KJH289g=") // hashed entry for 192.0.2.203:22

	// RSA
	rsaKey := genPublicKey(c)

	k, err := Unmarshal(&input)
	c.Assert(err, IsNil)

	c.Assert(k.HostKeyCallback(net.JoinHostPort(ipAddress, "22"), &net.TCPAddr{Port: 22}, ecdsaKey), IsNil)
	c.Assert(k.HostKeyCallback(net.JoinHostPort(ipAddress, "22"), &net.TCPAddr{Port: 22}, rsaKey), Equals, HostNotFoundError) // wrong key type
}

func (S) TestComments(c *C) {
	const sep = " "
	var input bytes.Buffer

	key := genPublicKey(c)
	keyBytes := bytes.TrimRight(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key)), "\n")

	// commented out host
	host1Addr := "101.102.103.72"
	input.WriteString("# ")
	input.WriteString(host1Addr)
	input.WriteString(sep)
	input.Write(keyBytes)
	input.WriteString("\n")

	k, err := Unmarshal(&input)
	c.Assert(err, IsNil)

	// Test HostKeyCallback
	addr := &net.TCPAddr{
		Port: 22,
	}
	c.Assert(k.HostKeyCallback(host1Addr+":22", addr, key), Equals, HostNotFoundError)
}

func (S) TestFuzz(c *C) {
	_, err := Unmarshal(strings.NewReader("[: 0"))
	c.Assert(err, NotNil)
}
