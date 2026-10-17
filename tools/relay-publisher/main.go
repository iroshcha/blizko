// A dedicated repo deploy key publishes signed endpoint metadata. It never
// receives GitHub account tokens or reads the application's message store.
package main

import (
	"blizko/core/discovery"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
)

const repository = "ssh://git@ssh.github.com:443/iroshcha/blizko.git"

var forwarded = regexp.MustCompile(`Forwarding (https://[a-zA-Z0-9.-]+)(?:/|\s)`)

func main() {
	root := flag.String("root", "", "Private publisher directory")
	logfile := flag.String("log", "", "Current Tuna log")
	initialize := flag.Bool("init", false, "Create keys; output public keys only")
	export := flag.Bool("export", false, "Output signed current address for initial manual publication")
	flag.Parse()
	if *root == "" {
		log.Fatal("root required")
	}
	if *initialize {
		if e := initKeys(*root); e != nil {
			log.Fatal(e)
		}
		return
	}
	if *export {
		u, e := endpoint(*logfile)
		if e != nil {
			log.Fatal(e)
		}
		key, e := os.ReadFile(filepath.Join(*root, "directory.key"))
		if e != nil || len(key) != 64 {
			log.Fatal("signing key missing")
		}
		now := time.Now()
		raw, e := discovery.Sign(discovery.Record{Version: 1, URL: u, Revision: now.UnixMilli(), Expires: now.Add(7 * 24 * time.Hour).UnixMilli()}, ed25519.PrivateKey(key))
		if e != nil {
			log.Fatal(e)
		}
		fmt.Println(string(raw))
		return
	}
	if e := publish(*root, *logfile); e != nil {
		log.Fatal(e)
	}
}
func initKeys(root string) error {
	if e := os.MkdirAll(root, 0700); e != nil {
		return e
	}
	signPath := filepath.Join(root, "directory.key")
	if _, e := os.Stat(signPath); os.IsNotExist(e) {
		_, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		if e = os.WriteFile(signPath, key, 0600); e != nil {
			return e
		}
	}
	sshPath := filepath.Join(root, "github.key")
	if _, e := os.Stat(sshPath); os.IsNotExist(e) {
		_, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		block, e := ssh.MarshalPrivateKey(key, "blizko-relay-publisher")
		if e != nil {
			return e
		}
		if e = os.WriteFile(sshPath, pem.EncodeToMemory(block), 0600); e != nil {
			return e
		}
	}
	key, e := os.ReadFile(signPath)
	if e != nil || len(key) != 64 {
		return errors.New("invalid signing key")
	}
	raw, e := os.ReadFile(sshPath)
	if e != nil {
		return e
	}
	signer, e := ssh.ParsePrivateKey(raw)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{"publicKeyHex": hex.EncodeToString(ed25519.PrivateKey(key).Public().(ed25519.PublicKey)), "deployKey": strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))})
}
func endpoint(logfile string) (string, error) {
	f, e := os.Open(logfile)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return "", e
	}
	start := st.Size() - 65536
	if start < 0 {
		start = 0
	}
	b := make([]byte, st.Size()-start)
	_, e = f.ReadAt(b, start)
	if e != nil {
		return "", e
	}
	matches := forwarded.FindAllSubmatch(b, -1)
	if len(matches) == 0 {
		return "", errors.New("Tuna has not announced a public address")
	}
	u := string(matches[len(matches)-1][1]) + "/"
	if !discovery.ValidURL(u) {
		return "", errors.New("invalid announced URL")
	}
	return u, nil
}
func publish(root, logfile string) error {
	u, e := endpoint(logfile)
	if e != nil {
		return e
	}
	// Do not publish a changed address just because a line appeared in a log:
	// the supervisor invokes us only while its current Tuna/server processes live.
	key, e := os.ReadFile(filepath.Join(root, "directory.key"))
	if e != nil || len(key) != 64 {
		return errors.New("signing key missing")
	}
	successPath := filepath.Join(root, "last-success.json")
	if raw, e := os.ReadFile(successPath); e == nil {
		old, e := discovery.Verify(raw, ed25519.PrivateKey(key).Public().(ed25519.PublicKey), time.Now())
		if e == nil && old.URL == u && old.Expires > time.Now().Add(5*24*time.Hour).UnixMilli() {
			fmt.Println("Relay address unchanged")
			return nil
		}
	}
	auth, e := gitssh.NewPublicKeysFromFile("git", filepath.Join(root, "github.key"), "")
	if e != nil {
		return e
	}
	auth.HostKeyAlgorithms = []string{ssh.KeyAlgoED25519}
	auth.HostKeyCallback = func(host string, remote net.Addr, key ssh.PublicKey) error {
		if ssh.FingerprintSHA256(key) != "SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU" {
			return errors.New("GitHub SSH host key mismatch")
		}
		return nil
	}
	dir := filepath.Join(root, "checkout")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	repo, e := git.PlainOpen(dir)
	if e == git.ErrRepositoryNotExists {
		repo, e = git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{URL: repository, Auth: auth, ReferenceName: plumbing.NewBranchReferenceName("relay-address"), SingleBranch: true, Depth: 1})
	}
	if e != nil {
		return e
	}
	work, e := repo.Worktree()
	if e != nil {
		return e
	}
	// Retry a previously committed but unpushed update before generating another.
	refspec := config.RefSpec("refs/heads/relay-address:refs/heads/relay-address")
	if e = repo.PushContext(ctx, &git.PushOptions{Auth: auth, RefSpecs: []config.RefSpec{refspec}}); e != nil && e != git.NoErrAlreadyUpToDate {
		return e
	}
	path := filepath.Join(dir, "relay.json")
	if raw, e := os.ReadFile(path); e == nil {
		old, e := discovery.Verify(raw, ed25519.PrivateKey(key).Public().(ed25519.PublicKey), time.Now())
		if e == nil && old.URL == u && old.Expires > time.Now().Add(5*24*time.Hour).UnixMilli() {
			_ = os.WriteFile(successPath, raw, 0600)
			fmt.Println("Relay address unchanged")
			return nil
		}
	}
	now := time.Now()
	raw, e := discovery.Sign(discovery.Record{Version: 1, URL: u, Revision: now.UnixMilli(), Expires: now.Add(7 * 24 * time.Hour).UnixMilli()}, ed25519.PrivateKey(key))
	if e != nil {
		return e
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		return e
	}
	if _, e = work.Add("relay.json"); e != nil {
		return e
	}
	if _, e = work.Commit("Update signed relay address", &git.CommitOptions{Author: &object.Signature{Name: "Blizko home server", Email: "blizko-relay@users.noreply.github.com", When: now}}); e != nil {
		return e
	}
	if e = repo.PushContext(ctx, &git.PushOptions{Auth: auth, RefSpecs: []config.RefSpec{refspec}}); e != nil {
		return e
	}
	if e = os.WriteFile(successPath, raw, 0600); e != nil {
		return e
	}
	fmt.Println("Published relay address", u)
	return nil
}
