package backends

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	account "github.com/jiujiu532/grok2api/app/control/account"
	"github.com/jiujiu532/grok2api/app/platform"
)

var supportedBackends = map[string]struct{}{
	"local":      {},
	"redis":      {},
	"mysql":      {},
	"postgresql": {},
}

type RepositoryConstructor func(string) (account.AccountRepository, error)

type RepositoryConstructors struct {
	Local      RepositoryConstructor
	Redis      RepositoryConstructor
	MySQL      RepositoryConstructor
	PostgreSQL RepositoryConstructor
}

func CreateRepository(
	env map[string]string,
	constructors RepositoryConstructors,
) (account.AccountRepository, error) {
	constructors = constructors.WithDefaults()
	backend, err := GetRepositoryBackend(env)
	if err != nil {
		return nil, err
	}
	return createRepositoryForBackend(env, constructors, backend)
}

func DescribeRepositoryTarget(env map[string]string) (string, string, error) {
	backend, err := GetRepositoryBackend(env)
	if err != nil {
		return "", "", err
	}
	switch backend {
	case "local":
		return "local", ResolveLocalDBPath(env), nil
	case "redis":
		value, err := requiredEnv(env, "ACCOUNT_REDIS_URL")
		if err != nil {
			return "", "", err
		}
		return "redis", RedactRepositoryURL(value), nil
	case "mysql":
		return "mysql", RedactRepositoryURL(envValue(env, "ACCOUNT_MYSQL_URL", "")), nil
	case "postgresql":
		value := envValue(env, "ACCOUNT_POSTGRESQL_URL", "")
		return "postgresql", RedactRepositoryURL(value), nil
	default:
		return backend, "<unknown>", nil
	}
}

func GetRepositoryBackend(env map[string]string) (string, error) {
	backend := strings.ToLower(envValue(env, "ACCOUNT_STORAGE", "local"))
	if _, ok := supportedBackends[backend]; !ok {
		return "", unknownBackendError(backend)
	}
	return backend, nil
}

func ResolveLocalDBPath(env map[string]string) string {
	raw := envValue(env, "ACCOUNT_LOCAL_PATH", platform.DataPath("accounts.db"))
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Join(projectRoot(), raw)
}

func RedactRepositoryURL(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return "<empty>"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return raw
	}
	return redactedParsedURL(parsed, raw)
}

func (constructors RepositoryConstructors) WithDefaults() RepositoryConstructors {
	if constructors.Local == nil {
		constructors.Local = newLocalRepository
	}
	if constructors.Redis == nil {
		constructors.Redis = newRedisRepository
	}
	if constructors.MySQL == nil {
		constructors.MySQL = newMySQLRepository
	}
	if constructors.PostgreSQL == nil {
		constructors.PostgreSQL = newPostgreSQLRepository
	}
	return constructors
}

func createRepositoryForBackend(
	env map[string]string,
	constructors RepositoryConstructors,
	backend string,
) (account.AccountRepository, error) {
	switch backend {
	case "local":
		return constructors.Local(ResolveLocalDBPath(env))
	case "redis":
		value, err := requiredEnv(env, "ACCOUNT_REDIS_URL")
		if err != nil {
			return nil, err
		}
		return constructors.Redis(value)
	case "mysql":
		return constructors.MySQL(envValue(env, "ACCOUNT_MYSQL_URL", ""))
	case "postgresql":
		value := envValue(env, "ACCOUNT_POSTGRESQL_URL", "")
		return constructors.PostgreSQL(value)
	default:
		return nil, unknownBackendError(backend)
	}
}

func redactedParsedURL(parsed *url.URL, raw string) string {
	host := parsed.Hostname()
	if port := parsed.Port(); port != "" {
		host = host + ":" + port
	}
	auth := redactedAuth(parsed.User)
	if host == "" {
		return raw
	}
	result := parsed.Scheme + "://" + auth + host + parsed.EscapedPath()
	if parsed.RawQuery != "" {
		result += "?" + parsed.RawQuery
	}
	if fragment := parsed.EscapedFragment(); fragment != "" {
		result += "#" + fragment
	}
	return result
}

func redactedAuth(user *url.Userinfo) string {
	if user == nil {
		return ""
	}
	username := user.Username()
	password, hasPassword := user.Password()
	if username != "" {
		return username + ":***@"
	}
	if hasPassword && password != "" {
		return "***@"
	}
	return ""
}

func envValue(env map[string]string, name, defaultValue string) string {
	var raw string
	var ok bool
	if env == nil {
		raw, ok = os.LookupEnv(name)
	} else {
		raw, ok = env[name]
	}
	if !ok {
		return defaultValue
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue
	}
	return raw
}

func requiredEnv(env map[string]string, name string) (string, error) {
	value := envValue(env, name, "")
	if value == "" {
		return "", fmt.Errorf("Missing required env: %s", name)
	}
	return value, nil
}

func projectRoot() string {
	_, file, _, ok := goruntime.Caller(0)
	if ok && filepath.IsAbs(file) {
		root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
		return filepath.Clean(root)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return filepath.Clean(wd)
}

func notMigratedConstructor(name string) RepositoryConstructor {
	return func(string) (account.AccountRepository, error) {
		return nil, fmt.Errorf("%s account repository backend is not migrated to Go yet", name)
	}
}

func unknownBackendError(backend string) error {
	return fmt.Errorf("Unknown account storage backend: '%s'", backend)
}
