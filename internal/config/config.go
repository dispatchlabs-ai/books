package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/dispatchlabs-ai/books/internal/money"

	"github.com/pelletier/go-toml/v2"
)

const CurrentVersion = 1

var (
	companyKeyPattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	databaseUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// Config is the process-wide Books configuration stored in ~/.books/books.toml.
// Company data stays in separately owned SQLite files beneath the Books home.
type Config struct {
	Version        int                `toml:"version"`
	DefaultCompany string             `toml:"default_company,omitempty"`
	Defaults       GlobalDefaults     `toml:"defaults,omitempty"`
	Companies      map[string]Company `toml:"companies,omitempty"`
}

type GlobalDefaults struct {
	Output string `toml:"output,omitempty"`
}

type Company struct {
	Name          string          `toml:"name"`
	EntityCode    string          `toml:"entity_code"`
	BookCode      string          `toml:"book_code"`
	DatabaseUUID  string          `toml:"database_uuid,omitempty"`
	Currency      string          `toml:"currency"`
	Basis         string          `toml:"basis"`
	FiscalYearEnd int             `toml:"fiscal_year_end_month"`
	Database      string          `toml:"database"`
	Attachments   string          `toml:"attachments"`
	Backups       string          `toml:"backups"`
	Imports       string          `toml:"imports"`
	Plans         string          `toml:"plans"`
	Defaults      CompanyDefaults `toml:"defaults,omitempty"`
}

type CompanyDefaults struct {
	PaymentAccount   string `toml:"payment_account,omitempty"`
	DepositAccount   string `toml:"deposit_account,omitempty"`
	RetainedEarnings string `toml:"retained_earnings,omitempty"`
}

type ResolvedCompany struct {
	Key         string
	Company     Company
	ConfigPath  string
	Home        string
	Database    string
	Attachments string
	Backups     string
	Imports     string
	Plans       string
}

func DefaultPath(explicit string) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return absolute(value)
	}
	if value := strings.TrimSpace(os.Getenv("BOOKS_CONFIG")); value != "" {
		return absolute(value)
	}
	if value := strings.TrimSpace(os.Getenv("BOOKS_HOME")); value != "" {
		home, err := absolute(value)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "books.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".books", "books.toml"), nil
}

func absolute(path string) (string, error) {
	result, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve absolute path %q: %w", path, err)
	}
	return result, nil
}

func New() Config {
	return Config{
		Version:   CurrentVersion,
		Defaults:  GlobalDefaults{Output: "table"},
		Companies: make(map[string]Company),
	}
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("books configuration was not found at %s: %w", path, err)
		}
		return Config{}, fmt.Errorf("read Books configuration: %w", err)
	}
	var result Config
	decoder := toml.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Config{}, fmt.Errorf("decode Books configuration %s: %w", path, err)
	}
	if result.Version != CurrentVersion {
		return Config{}, fmt.Errorf("books configuration version %d is unsupported; expected %d", result.Version, CurrentVersion)
	}
	if result.Companies == nil {
		result.Companies = make(map[string]Company)
	}
	if err := result.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate Books configuration %s: %w", path, err)
	}
	return result, nil
}

func (c Config) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("version must be %d", CurrentVersion)
	}
	if c.Defaults.Output != "" && c.Defaults.Output != "table" && c.Defaults.Output != "json" && c.Defaults.Output != "jsonl" && c.Defaults.Output != "csv" {
		return fmt.Errorf("defaults.output must be table, json, jsonl, or csv")
	}
	for key, company := range c.Companies {
		if err := ValidateCompanyKey(key); err != nil {
			return err
		}
		if strings.TrimSpace(company.Name) == "" || strings.TrimSpace(company.EntityCode) == "" || strings.TrimSpace(company.BookCode) == "" {
			return fmt.Errorf("company %q requires name, entity_code, and book_code", key)
		}
		if company.DatabaseUUID != "" && !databaseUUIDPattern.MatchString(company.DatabaseUUID) {
			return fmt.Errorf("company %q database_uuid must be a canonical lowercase UUID", key)
		}
		paths := []struct{ label, value string }{
			{"database", company.Database}, {"attachments", company.Attachments}, {"backups", company.Backups},
			{"imports", company.Imports}, {"plans", company.Plans},
		}
		for _, path := range paths {
			if strings.TrimSpace(path.value) == "" {
				return fmt.Errorf("company %q requires a %s path", key, path.label)
			}
		}
		if !money.IsSupportedCurrency(company.Currency) {
			return fmt.Errorf("company %q uses unsupported currency %q; this release supports USD only", key, company.Currency)
		}
		if company.Basis != "ACCRUAL" && company.Basis != "CASH" {
			return fmt.Errorf("company %q basis must be ACCRUAL or CASH", key)
		}
		if company.FiscalYearEnd < 1 || company.FiscalYearEnd > 12 {
			return fmt.Errorf("company %q requires fiscal_year_end_month between 1 and 12", key)
		}
	}
	if c.DefaultCompany != "" {
		if _, ok := c.Companies[c.DefaultCompany]; !ok {
			return fmt.Errorf("default company %q is not registered", c.DefaultCompany)
		}
	}
	return nil
}

func Save(path string, value Config) error {
	return withLock(path, func() error {
		return saveUnlocked(path, value)
	})
}

// Update serializes one complete read-modify-publish operation across Books
// processes. When initial is non-nil, it is used only when path does not yet
// exist. The callback runs while the adjacent lock is held and receives whether
// the configuration existed before this update.
func Update(path string, initial *Config, mutate func(*Config, bool) error) (Config, error) {
	var result Config
	err := withLock(path, func() error {
		value, err := Load(path)
		existed := err == nil
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) || initial == nil {
				return err
			}
			value = *initial
		}
		if err := mutate(&value, existed); err != nil {
			return err
		}
		if err := saveUnlocked(path, value); err != nil {
			return err
		}
		result = value
		return nil
	})
	return result, err
}

func withLock(path string, action func() error) error {
	directory := filepath.Dir(path)
	if err := ensureDirectory(directory); err != nil {
		return err
	}
	lockPath := filepath.Join(directory, "."+filepath.Base(path)+".lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open Books configuration lock: %w", err)
	}
	defer func(closer interface{ Close() error }) { _ = closer.Close() }(lock)
	if err := lock.Chmod(0o600); err != nil {
		return fmt.Errorf("secure Books configuration lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock Books configuration: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	return action()
}

func ensureDirectory(directory string) error {
	_, directoryErr := os.Stat(directory)
	directoryCreated := errors.Is(directoryErr, os.ErrNotExist)
	if directoryErr != nil && !directoryCreated {
		return fmt.Errorf("inspect Books home: %w", directoryErr)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Books home: %w", err)
	}
	if directoryCreated {
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("secure Books home: %w", err)
		}
	}
	return nil
}

func saveUnlocked(path string, value Config) error {
	if err := value.Validate(); err != nil {
		return err
	}
	data, err := toml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode Books configuration: %w", err)
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".books.toml-*")
	if err != nil {
		return fmt.Errorf("create temporary Books configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary Books configuration: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write Books configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync Books configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Books configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish Books configuration: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure Books configuration: %w", err)
	}
	return nil
}

func ValidateCompanyKey(value string) error {
	if len(value) < 2 || len(value) > 32 {
		return fmt.Errorf("company key %q must contain between 2 and 32 characters", value)
	}
	if !companyKeyPattern.MatchString(value) {
		return fmt.Errorf("company key %q must contain lowercase letters, digits, and single hyphens only", value)
	}
	return nil
}

func DeriveCompanyKey(name string) string {
	value := strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	lastHyphen := false
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			builder.WriteRune(character)
			lastHyphen = false
			continue
		}
		if builder.Len() > 0 && !lastHyphen {
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if len(result) == 1 {
		result += "-co"
	}
	if len(result) > 32 {
		result = strings.TrimRight(result[:32], "-")
	}
	return result
}

func DeriveEntityCode(key string) string {
	return strings.ToUpper(strings.TrimSpace(key))
}

func NewCompany(key, name, currency, basis string) Company {
	prefix := filepath.Join("companies", key)
	entityCode := DeriveEntityCode(key)
	return Company{
		Name:          strings.TrimSpace(name),
		EntityCode:    entityCode,
		BookCode:      entityCode,
		Currency:      strings.ToUpper(strings.TrimSpace(currency)),
		Basis:         strings.ToUpper(strings.TrimSpace(basis)),
		FiscalYearEnd: 12,
		Database:      filepath.Join(prefix, "ledger.sqlite"),
		Attachments:   filepath.Join(prefix, "attachments"),
		Backups:       filepath.Join(prefix, "backups"),
		Imports:       filepath.Join(prefix, "imports"),
		Plans:         filepath.Join(prefix, "plans"),
	}
}

func (c Config) Resolve(configPath, selected string) (ResolvedCompany, error) {
	key := strings.ToLower(strings.TrimSpace(selected))
	if key == "" {
		key = c.DefaultCompany
	}
	if key == "" {
		return ResolvedCompany{}, fmt.Errorf("no default company is configured; supply --company COMPANY")
	}
	company, ok := c.Companies[key]
	if !ok {
		keys := c.CompanyKeys()
		return ResolvedCompany{}, fmt.Errorf("company %q is not registered; available companies: %s", key, strings.Join(keys, ", "))
	}
	home := filepath.Dir(configPath)
	resolve := func(value string) string {
		if filepath.IsAbs(value) {
			return filepath.Clean(value)
		}
		return filepath.Join(home, filepath.Clean(value))
	}
	return ResolvedCompany{
		Key: key, Company: company, ConfigPath: configPath, Home: home,
		Database: resolve(company.Database), Attachments: resolve(company.Attachments),
		Backups: resolve(company.Backups), Imports: resolve(company.Imports), Plans: resolve(company.Plans),
	}, nil
}

func (c Config) CompanyKeys() []string {
	result := make([]string, 0, len(c.Companies))
	for key := range c.Companies {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func EnsureCompanyDirectories(resolved ResolvedCompany) error {
	for _, directory := range []string{filepath.Dir(resolved.Database), resolved.Attachments, resolved.Backups, resolved.Imports, resolved.Plans} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create company directory %s: %w", directory, err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("secure company directory %s: %w", directory, err)
		}
	}
	return nil
}
