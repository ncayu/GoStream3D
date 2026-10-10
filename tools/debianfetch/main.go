package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz"
)

const indexPath = "main/binary-amd64/Packages.xz"

var repositories = []repository{
	{Name: "trixie", Base: "https://deb.debian.org/debian", Suite: "trixie"},
	{Name: "trixie-updates", Base: "https://deb.debian.org/debian", Suite: "trixie-updates"},
	{Name: "trixie-security", Base: "https://deb.debian.org/debian-security", Suite: "trixie-security"},
}

// Runtime inputs only. Neko and its patched Xorg modules are built separately
// from the source tree already checked into this workspace.
var roots = []string{
	"ca-certificates", "chromium", "xserver-xorg-core", "xserver-xorg-video-dummy",
	"pulseaudio", "dbus-x11", "xdotool", "xclip", "libgtk-3-0t64",
	"gstreamer1.0-plugins-base", "gstreamer1.0-plugins-good",
	"gstreamer1.0-plugins-bad", "gstreamer1.0-plugins-ugly", "gstreamer1.0-pulseaudio",
	"libcairo2", "libxcb1", "libxrandr2", "libxv1", "libopus0",
	"libvpx9", "libx264-164", "libvo-aacenc0", "librtmp1", "libxcvt0",
	"fonts-wqy-microhei", "fonts-noto-color-emoji", "fonts-liberation",
}

var buildRoots = []string{
	"libx11-dev", "libxrandr-dev", "libxtst-dev", "libgtk-3-dev", "libxcvt-dev",
	"libgstreamer1.0-dev", "libgstreamer-plugins-base1.0-dev",
	"gcc", "pkgconf", "autoconf", "automake", "libtool", "make", "patch",
	"xorg-dev", "xutils-dev",
}

type repository struct{ Name, Base, Suite string }

type packageRecord struct {
	Name, Version, Arch, Filename, SHA256, Depends, PreDepends, Provides, Repository string
	Size                                                                             int64
}

type manifestPackage struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
	Repository   string `json:"repository"`
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	LocalFile    string `json:"local_file"`
}

type manifest struct {
	Target      string            `json:"target"`
	GeneratedAt string            `json:"generated_at"`
	Roots       []string          `json:"roots"`
	Packages    []manifestPackage `json:"packages"`
	TotalBytes  int64             `json:"total_bytes"`
	Unresolved  []string          `json:"unresolved_dependencies,omitempty"`
	Note        string            `json:"note"`
}

func main() {
	out := flag.String("out", "../../third_party/debian-trixie-amd64", "local package cache directory")
	plan := flag.Bool("plan", false, "resolve and print package plan without downloading .deb files")
	withBuild := flag.Bool("build", false, "also cache Neko/Xorg compilation dependencies")
	verify := flag.Bool("verify", false, "verify saved .deb files against the local manifest without network access")
	flag.Parse()
	if *verify {
		if err := verifyCache(*out, *withBuild); err != nil {
			fmt.Fprintln(os.Stderr, "debianfetch:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(*out, *plan, *withBuild); err != nil {
		fmt.Fprintln(os.Stderr, "debianfetch:", err)
		os.Exit(1)
	}
}

func verifyCache(out string, withBuild bool) error {
	manifestName := "manifest.json"
	if withBuild {
		manifestName = "manifest-build.json"
	}
	data, err := os.ReadFile(filepath.Join(out, manifestName))
	if err != nil {
		return err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	var total int64
	for _, item := range m.Packages {
		if !strings.HasPrefix(item.LocalFile, "packages/") || strings.Contains(item.LocalFile, "..") {
			return fmt.Errorf("invalid local path for %s", item.Name)
		}
		if !fileMatches(filepath.Join(out, filepath.FromSlash(item.LocalFile)), item.SHA256, item.Size) {
			return fmt.Errorf("missing or corrupt package %s", item.Name)
		}
		total += item.Size
	}
	if total != m.TotalBytes {
		return fmt.Errorf("manifest size mismatch: %d != %d", total, m.TotalBytes)
	}
	fmt.Printf("Verified %d packages (%d bytes) without network access\n", len(m.Packages), total)
	return nil
}

func run(out string, plan, withBuild bool) error {
	client := &http.Client{Timeout: 10 * time.Minute}
	if err := os.MkdirAll(filepath.Join(out, "indexes"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(out, "packages"), 0755); err != nil {
		return err
	}
	available := make(map[string]packageRecord)
	providers := make(map[string]string)
	for _, repo := range repositories {
		fmt.Println("Reading", repo.Name, "package index")
		entries, err := fetchIndex(client, out, repo)
		if err != nil {
			return fmt.Errorf("%s: %w", repo.Name, err)
		}
		for _, p := range entries {
			// Later repositories override base packages when they publish updates.
			available[p.Name] = p
			for _, provided := range strings.Split(p.Provides, ",") {
				name := dependencyName(provided)
				if name != "" {
					if _, exists := providers[name]; !exists {
						providers[name] = p.Name
					}
				}
			}
		}
	}
	selectedRoots := append([]string(nil), roots...)
	if withBuild {
		selectedRoots = append(selectedRoots, buildRoots...)
	}
	selected, unresolved := resolve(available, providers, selectedRoots)
	if len(unresolved) != 0 {
		return fmt.Errorf("unresolved dependencies: %s", strings.Join(unresolved, "; "))
	}
	items := make([]manifestPackage, 0, len(selected))
	var total int64
	for _, p := range selected {
		repo := repositoryByName(p.Repository)
		if repo == nil {
			return fmt.Errorf("unknown repository %q", p.Repository)
		}
		if !strings.HasPrefix(p.Filename, "pool/") || strings.Contains(p.Filename, "..") {
			return fmt.Errorf("unsafe package filename %q", p.Filename)
		}
		name := filepath.Base(p.Filename)
		items = append(items, manifestPackage{
			Name: p.Name, Version: p.Version, Architecture: p.Arch, Repository: p.Repository,
			URL: repo.Base + "/" + p.Filename, SHA256: p.SHA256, Size: p.Size,
			LocalFile: "packages/" + name,
		})
		total += p.Size
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	fmt.Printf("Resolved %d packages, %.1f MiB compressed\n", len(items), float64(total)/1048576)
	if plan {
		return nil
	}
	ctx := context.Background()
	jobs := make(chan manifestPackage)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if err := downloadPackage(ctx, client, out, item); err != nil {
					select {
					case errCh <- err:
					default:
					}
				}
			}
		}()
	}
	for _, item := range items {
		jobs <- item
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
	}
	m := manifest{
		Target: "Debian 13 (trixie) amd64", GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Roots: selectedRoots, Packages: items, TotalBytes: total,
		Note: "Neko 3.1.5 and patched Xorg modules must be built on Linux. Installability must be tested on the target Debian 13 host.",
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	manifestName := "manifest.json"
	if withBuild {
		manifestName = "manifest-build.json"
	}
	if err := os.WriteFile(filepath.Join(out, manifestName), append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Println("Verified and saved all packages in", out)
	return nil
}

func repositoryByName(name string) *repository {
	for i := range repositories {
		if repositories[i].Name == name {
			return &repositories[i]
		}
	}
	return nil
}

func fetchIndex(client *http.Client, out string, repo repository) ([]packageRecord, error) {
	releaseURL := repo.Base + "/dists/" + repo.Suite + "/InRelease"
	release, err := getBytes(client, releaseURL)
	if err != nil {
		return nil, err
	}
	wantHash, wantSize, err := releaseChecksum(string(release), indexPath)
	if err != nil {
		return nil, err
	}
	indexFile := filepath.Join(out, "indexes", repo.Name+"-Packages.xz")
	indexURL := repo.Base + "/dists/" + repo.Suite + "/" + indexPath
	if !fileMatches(indexFile, wantHash, wantSize) {
		if err := saveURL(client, indexURL, indexFile, wantHash, wantSize); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(indexFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := xz.NewReader(f)
	if err != nil {
		return nil, err
	}
	return parsePackages(zr, repo.Name)
}

func releaseChecksum(release, path string) (string, int64, error) {
	inSHA := false
	for _, line := range strings.Split(release, "\n") {
		if line == "SHA256:" {
			inSHA = true
			continue
		}
		if inSHA && !strings.HasPrefix(line, " ") {
			inSHA = false
		}
		if !inSHA {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] != path {
			continue
		}
		size, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return "", 0, err
		}
		return fields[0], size, nil
	}
	return "", 0, fmt.Errorf("%s missing from InRelease SHA256 list", path)
}

func parsePackages(r io.Reader, repo string) ([]packageRecord, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	fields := make(map[string]string)
	var result []packageRecord
	flush := func() error {
		if len(fields) == 0 {
			return nil
		}
		if fields["Package"] == "" || fields["Filename"] == "" || fields["SHA256"] == "" {
			return errors.New("incomplete package entry")
		}
		size, err := strconv.ParseInt(fields["Size"], 10, 64)
		if err != nil {
			return err
		}
		result = append(result, packageRecord{
			Name: fields["Package"], Version: fields["Version"], Arch: fields["Architecture"],
			Filename: fields["Filename"], SHA256: fields["SHA256"], Size: size,
			Depends: fields["Depends"], PreDepends: fields["Pre-Depends"], Provides: fields["Provides"], Repository: repo,
		})
		fields = make(map[string]string)
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasPrefix(line, " ") {
			continue
		}
		key, value, ok := strings.Cut(line, ": ")
		if ok {
			fields[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return result, nil
}

func resolve(available map[string]packageRecord, providers map[string]string, rootPackages []string) (map[string]packageRecord, []string) {
	selected := make(map[string]packageRecord)
	queue := append([]string(nil), rootPackages...)
	var missing []string
	for len(queue) != 0 {
		name := queue[0]
		queue = queue[1:]
		if _, done := selected[name]; done {
			continue
		}
		p, found := available[name]
		if !found {
			missing = append(missing, name)
			continue
		}
		selected[name] = p
		for _, group := range strings.Split(strings.Join([]string{p.PreDepends, p.Depends}, ","), ",") {
			if strings.TrimSpace(group) == "" {
				continue
			}
			chosen := ""
			for _, alt := range strings.Split(group, "|") {
				candidate := dependencyName(alt)
				if _, ok := available[candidate]; ok {
					chosen = candidate
					break
				}
				if provider, ok := providers[candidate]; ok {
					chosen = provider
					break
				}
			}
			if chosen == "" {
				missing = append(missing, p.Name+": "+strings.TrimSpace(group))
			} else {
				queue = append(queue, chosen)
			}
		}
	}
	sort.Strings(missing)
	return selected, missing
}

func dependencyName(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.IndexAny(value, " ([<"); i >= 0 {
		value = value[:i]
	}
	if i := strings.IndexByte(value, ':'); i >= 0 {
		value = value[:i]
	}
	return value
}

func getBytes(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
}

func saveURL(client *http.Client, url, path, wantHash string, wantSize int64) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %s", url, resp.Status)
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != wantSize || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), wantHash) {
		return fmt.Errorf("checksum mismatch for %s", url)
	}
	return os.Rename(tmp, path)
}

func downloadPackage(ctx context.Context, client *http.Client, out string, item manifestPackage) error {
	path := filepath.Join(out, filepath.FromSlash(item.LocalFile))
	if fileMatches(path, item.SHA256, item.Size) {
		return nil
	}
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				tmp := path + ".part"
				f, openErr := os.Create(tmp)
				if openErr != nil {
					resp.Body.Close()
					return openErr
				}
				h := sha256.New()
				n, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
				closeErr := f.Close()
				resp.Body.Close()
				if copyErr == nil && closeErr == nil && n == item.Size && strings.EqualFold(hex.EncodeToString(h.Sum(nil)), item.SHA256) {
					if err := os.Rename(tmp, path); err != nil {
						return err
					}
					fmt.Println("Saved", item.Name)
					return nil
				}
				err = fmt.Errorf("size or SHA256 mismatch for %s", item.Name)
			} else {
				err = fmt.Errorf("%s: HTTP %s", item.URL, resp.Status)
				resp.Body.Close()
			}
		}
		if attempt == 3 {
			return err
		}
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return errors.New("unreachable")
}

func fileMatches(path, wantHash string, wantSize int64) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() != wantSize {
		return false
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), wantHash)
}
