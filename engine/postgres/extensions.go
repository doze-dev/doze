package postgres

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// contrib is the set of extensions bundled with a standard Postgres build.
// CREATE EXTENSION works for these out of the box; doze never installs a binary
// for them.
var contrib = map[string]bool{
	"adminpack": true, "amcheck": true, "autoinc": true, "bloom": true,
	"btree_gin": true, "btree_gist": true, "citext": true, "cube": true,
	"dblink": true, "dict_int": true, "dict_xsyn": true, "earthdistance": true,
	"file_fdw": true, "fuzzystrmatch": true, "hstore": true, "insert_username": true,
	"intagg": true, "intarray": true, "isn": true, "lo": true, "ltree": true,
	"moddatetime": true, "old_snapshot": true, "pageinspect": true,
	"pg_buffercache": true, "pg_freespacemap": true, "pg_prewarm": true,
	"pg_stat_statements": true, "pg_surgery": true, "pg_trgm": true,
	"pg_visibility": true, "pgcrypto": true, "pgrowlocks": true, "pgstattuple": true,
	"plpgsql": true, "postgres_fdw": true, "refint": true, "seg": true,
	"sslinfo": true, "tablefunc": true, "tcn": true, "tsm_system_rows": true,
	"tsm_system_time": true, "unaccent": true, "uuid-ossp": true, "xml2": true,
}

// installer lays prebuilt extension bundles into a toolchain.
type installer struct {
	pgConfig string
	http     *http.Client
}

func newInstaller(pgConfig string) *installer {
	return &installer{pgConfig: pgConfig, http: &http.Client{Timeout: 5 * time.Minute}}
}

// available reports whether CREATE EXTENSION can already find the named
// extension (it is contrib or already installed).
func (in *installer) available(name string) bool {
	share, err := in.dir("--sharedir")
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(share, "extension", name+".control"))
	return err == nil
}

// install fetches a bundle from source (a local path or http(s) URL) and lays
// its files into the toolchain's share and lib directories. The bundle is a
// .tar.gz whose entries live under "share/" and "lib/".
func (in *installer) install(name, source string) error {
	data, err := in.read(source)
	if err != nil {
		return fmt.Errorf("reading extension bundle %q: %w", source, err)
	}
	share, err := in.dir("--sharedir")
	if err != nil {
		return err
	}
	pkglib, err := in.dir("--pkglibdir")
	if err != nil {
		return err
	}
	if err := extractBundle(data, share, pkglib); err != nil {
		return fmt.Errorf("installing extension %q: %w", name, err)
	}
	if !in.available(name) {
		return fmt.Errorf("extension %q still not available after installing %q "+
			"(bundle should contain share/extension/%s.control)", name, source, name)
	}
	return nil
}

func (in *installer) read(source string) ([]byte, error) {
	if strings.Contains(source, "://") {
		resp, err := in.http.Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: %s", source, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(source)
}

func (in *installer) dir(flag string) (string, error) {
	out, err := exec.Command(in.pgConfig, flag).Output()
	if err != nil {
		return "", fmt.Errorf("pg_config %s: %w", flag, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// extractBundle unpacks a bundle, routing share/* under shareDir and lib/* under
// pkglibDir.
func extractBundle(data []byte, shareDir, pkglibDir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		clean := filepath.Clean(hdr.Name)
		var dest string
		switch {
		case strings.HasPrefix(clean, "share/"):
			dest = filepath.Join(shareDir, strings.TrimPrefix(clean, "share/"))
		case strings.HasPrefix(clean, "lib/"):
			dest = filepath.Join(pkglibDir, strings.TrimPrefix(clean, "lib/"))
		default:
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}
