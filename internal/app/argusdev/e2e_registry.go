package argusdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const registryManifestAccept = "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json"

type registryReference struct {
	baseURL    string
	repository string
	tag        string
}

func (a *App) removeRemoteE2EImages(ctx context.Context, env *E2EEnvironment) error {
	images, err := remoteE2EImagesForCleanup(env.ConfigPath, env.ImageTag, env.State.FixtureImages)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	var cleanupErrors []error
	for _, image := range images {
		if err := deleteRegistryTag(ctx, client, image); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("delete registry image %s: %w", image, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func remoteE2EImagesForCleanup(configPath, imageTag string, fixtureImages map[string]string) ([]string, error) {
	registry, err := e2eRegistry(configPath)
	if err != nil {
		return nil, err
	}
	registry = strings.TrimSuffix(registry, "/")
	seen := map[string]bool{}
	images := make([]string, 0, len(fixtureImages)+3)
	add := func(image string) {
		if image != "" && !seen[image] {
			seen[image] = true
			images = append(images, image)
		}
	}
	for _, name := range []string{"argus-backend", "argus-web", "minio"} {
		add(registry + "/argus/" + name + ":" + imageTag)
	}
	for _, image := range fixtureImages {
		add(image)
	}
	sort.Strings(images)
	return images, nil
}

func deleteRegistryTag(ctx context.Context, client *http.Client, image string) error {
	reference, err := parseRegistryReference(image)
	if err != nil {
		return err
	}
	digest, found, err := registryManifestDigest(ctx, client, reference, reference.tag)
	if err != nil || !found {
		return err
	}
	tags, err := registryTags(ctx, client, reference)
	if err != nil {
		return err
	}
	for _, tag := range tags {
		if tag == reference.tag {
			continue
		}
		otherDigest, otherFound, err := registryManifestDigest(ctx, client, reference, tag)
		if err != nil {
			return fmt.Errorf("resolve sibling tag %q: %w", tag, err)
		}
		if otherFound && otherDigest == digest {
			return fmt.Errorf("tag shares manifest %s with %q; refusing digest deletion", digest, tag)
		}
	}
	requestURL := reference.baseURL + "/v2/" + escapeRepository(reference.repository) + "/manifests/" + url.PathEscape(digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, requestURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("registry delete returned %s", response.Status)
	}
	return nil
}

func parseRegistryReference(image string) (registryReference, error) {
	registry, remainder, found := strings.Cut(strings.TrimSpace(image), "/")
	if !found || registry == "" || remainder == "" {
		return registryReference{}, fmt.Errorf("invalid registry image reference %q", image)
	}
	lastSlash := strings.LastIndex(remainder, "/")
	tagSeparator := strings.LastIndex(remainder, ":")
	if tagSeparator <= lastSlash || tagSeparator == len(remainder)-1 {
		return registryReference{}, fmt.Errorf("registry image reference must contain an explicit tag: %q", image)
	}
	repository := remainder[:tagSeparator]
	tag := remainder[tagSeparator+1:]
	if strings.Contains(tag, "@") || strings.Contains(repository, "@") {
		return registryReference{}, fmt.Errorf("digest references are not valid E2E cleanup targets: %q", image)
	}
	host := registry
	if _, port, err := net.SplitHostPort(registry); err == nil {
		host = net.JoinHostPort("127.0.0.1", port)
	}
	return registryReference{baseURL: "http://" + host, repository: repository, tag: tag}, nil
}

func registryManifestDigest(ctx context.Context, client *http.Client, reference registryReference, manifest string) (string, bool, error) {
	requestURL := reference.baseURL + "/v2/" + escapeRepository(reference.repository) + "/manifests/" + url.PathEscape(manifest)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, requestURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", registryManifestAccept)
	response, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", false, fmt.Errorf("registry manifest lookup returned %s", response.Status)
	}
	digest := strings.TrimSpace(response.Header.Get("Docker-Content-Digest"))
	if digest == "" {
		return "", false, fmt.Errorf("registry manifest response omitted Docker-Content-Digest")
	}
	return digest, true, nil
}

func registryTags(ctx context.Context, client *http.Client, reference registryReference) ([]string, error) {
	requestURL := reference.baseURL + "/v2/" + escapeRepository(reference.repository) + "/tags/list"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil, fmt.Errorf("registry tag listing returned %s", response.Status)
	}
	var payload struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode registry tag listing: %w", err)
	}
	return payload.Tags, nil
}

func escapeRepository(repository string) string {
	parts := strings.Split(repository, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}
