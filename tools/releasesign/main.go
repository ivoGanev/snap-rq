// Command releasesign makes the release signing key and signs release files.
//
//	go run ./tools/releasesign keygen <private-key-file>
//	    Writes a new key pair: the private key to <private-key-file> (keep it
//	    offline, and as the release environment's RELEASE_SIGNING_KEY secret),
//	    the public key to backend/selfupdate/release_key.pub.
//	RELEASE_SIGNING_KEY=<private key> go run ./tools/releasesign sign <file>...
//	    Writes <file>.sig next to each file.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"snap-rq/backend/selfupdate"
)

const publicKeyFile = "backend/selfupdate/release_key.pub"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "releasesign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: releasesign keygen <private-key-file> | releasesign sign <file>...")
	}
	switch args[0] {
	case "keygen":
		return keygen(args[1])
	case "sign":
		return sign(args[1:])
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func keygen(privatePath string) error {
	if _, err := os.Stat(privatePath); err == nil {
		return fmt.Errorf("%s exists; a new key would orphan every installed copy", privatePath)
	}
	if _, err := selfupdate.ReleasePublicKey(); err == nil {
		return fmt.Errorf("%s already holds a key; a new key would orphan every installed copy", publicKeyFile)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(private.Seed())
	if err := os.WriteFile(privatePath, []byte(encoded+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(publicKeyFile, []byte(base64.StdEncoding.EncodeToString(public)+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("private key: %s\npublic key:  %s\n", privatePath, publicKeyFile)
	return nil
}

func sign(files []string) error {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("RELEASE_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("RELEASE_SIGNING_KEY is missing or malformed")
	}
	key := ed25519.NewKeyFromSeed(seed)
	pinned, err := selfupdate.ReleasePublicKey()
	if err != nil {
		return err
	}
	if !key.Public().(ed25519.PublicKey).Equal(pinned) {
		return errors.New("RELEASE_SIGNING_KEY doesn't match " + publicKeyFile + ": installed apps would reject the update")
	}
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if err := os.WriteFile(file+selfupdate.SignatureSuffix, []byte(selfupdate.Sign(key, contents)+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Println("signed", file)
	}
	return nil
}
