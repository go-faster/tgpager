package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-faster/errors"
	"github.com/go-faster/figureout"
)

// Secret is a credential, written as a literal value, as the name of an
// environment variable, or as the path to a file holding it.
//
// The file spelling is the half of secret handling an environment override
// cannot cover: Kubernetes, Docker and systemd's LoadCredential all hand a
// process a path, not a variable.
//
// It is a wire shape. [Secret.resolve] materializes it, so everything
// downstream reads [Secret.Value] whichever spelling was used.
type Secret struct {
	Value string
	Env   string
	File  string
}

// secretDescriptor describes the object spelling. The scalar spelling is
// widened into Value by [figureout.ScalarOr].
var secretDescriptor = figureout.MustDerive(
	func(c *Secret, s *figureout.Schema[Secret]) {
		figureout.Value(s, &c.Value, "value", figureout.Secret()).
			Doc("Literal value. Prefer env or file outside development.")
		figureout.Value(s, &c.Env, "env").
			Doc("Name of the environment variable holding the value.")
		figureout.Value(s, &c.File, "file").
			Doc("Path to a file holding the value, relative to the config file.")
	},
)

// secret registers a credential: a bare scalar, or {value|env|file}.
func secret[R any](s *figureout.Schema[R], field *Secret, name string, opts ...figureout.FieldOption) *figureout.ObjectField {
	return figureout.ScalarOr(s, field, name, secretDescriptor,
		func(v string) Secret { return Secret{Value: v} }, opts...)
}

// resolve materializes the secret into Value.
func (s *Secret) resolve(baseDir string) error {
	set := 0
	for _, spelling := range []string{s.Value, s.Env, s.File} {
		if spelling != "" {
			set++
		}
	}
	switch {
	case set == 0:
		return nil
	case set > 1:
		return errors.New("set only one of value, env or file")
	case s.Env != "":
		s.Value = os.Getenv(s.Env)
	case s.File != "":
		path := s.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return errors.Wrap(err, "read secret file")
		}
		// A secret written with "echo" ends in a newline that is not part of it.
		s.Value = strings.TrimRight(string(data), "\r\n")
	}
	s.Env, s.File = "", ""
	return nil
}
