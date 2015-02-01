package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

const dataFile = "slugbuilder/builder/buildpacks.txt"

func main() {
	log.SetFlags(log.Lshortfile)

	raw, err := ioutil.ReadFile(dataFile)
	if err != nil {
		log.Fatal(err)
	}

	commitMsg, out := &bytes.Buffer{}, &bytes.Buffer{}
	modified := false
	commitMsg.WriteString("slugbuilder: Bump buildpacks\n\n")
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		splitLine := bytes.SplitN(line, []byte("#"), 2)
		if len(splitLine) != 2 {
			log.Fatalf("error parsing line %q", string(line))
		}

		repo, ref := strings.TrimSuffix(string(splitLine[0]), ".git"), string(splitLine[1])
		newRef := getMaster(repo)
		if !strings.HasPrefix(newRef, ref) {
			modified = true
			fmt.Fprintf(commitMsg, "%s/compare/%s...%s\n", strings.TrimSuffix(repo, ".git"), ref, newRef[:8])
			ref = newRef[:8]
		}
		fmt.Fprintf(out, "%s#%s\n", string(splitLine[0]), ref)
	}
	if !modified {
		log.Println("no updates")
		os.Exit(0)
	}

	if err := ioutil.WriteFile(dataFile, out.Bytes(), 0644); err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(commitMsg, "\nSigned-off-by: %s <%s>\n", gitConfig("user.name"), gitConfig("user.email"))
	cmd := exec.Command("git", "commit", "-m", commitMsg.String(), "--", dataFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func getMaster(repo string) string {
	u, err := url.Parse(repo)
	if err != nil {
		log.Fatal(err)
	}

	refURL := fmt.Sprintf("https://api.github.com/repos%s/git/refs/heads/master", strings.TrimSuffix(u.Path, ".git"))
	res, err := http.Get(refURL)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		log.Fatal(&url.Error{
			Op:  "GET",
			URL: refURL,
			Err: fmt.Errorf("unexpected status %d", res.StatusCode),
		})
	}
	var data struct {
		Object struct {
			SHA string
		}
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		log.Fatal(err)
	}
	return data.Object.SHA
}

func gitConfig(key string) string {
	cmd := exec.Command("git", "config", "--global", key)
	out, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes.TrimSpace(out))
}
