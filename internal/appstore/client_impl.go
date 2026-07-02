package appstore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/99designs/keyring"
	cookiejar "github.com/juju/persistent-cookiejar"
	ipaappstore "github.com/majd/ipatool/v2/pkg/appstore"
	ipakeychain "github.com/majd/ipatool/v2/pkg/keychain"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	"github.com/schollz/progressbar/v3"

	"github.com/yeuleh/ipa-manager/internal/account"
)

// keyringOpener is the function used to open keyring backends.
// Package-level variable allows test injection (Spok finding 4).
var keyringOpener = keyring.Open

// profileAppStoreAdapter wraps ipatool's AppStore behind our ProfileAppStore
// interface. This is the ONLY place in the codebase that imports ipatool's
// appstore package for method calls. All ipatool API changes are confined here.
type profileAppStoreAdapter struct {
	inner   ipaappstore.AppStore
	account *ipaappstore.Account // cached after AccountInfo(); used by Lookup/Search/Download/Purchase
}

func (a *profileAppStoreAdapter) GetAuthEndpoint() (string, error) {
	bag, err := a.inner.Bag(ipaappstore.BagInput{})
	if err != nil {
		return "", err
	}
	return bag.AuthEndpoint, nil
}

func (a *profileAppStoreAdapter) Login(input LoginInput) (LoginResult, error) {
	output, err := a.inner.Login(ipaappstore.LoginInput{
		Email:    input.Email,
		Password: input.Password,
		AuthCode: input.AuthCode,
		Endpoint: input.Endpoint,
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		Name:       output.Account.Name,
		Email:      output.Account.Email,
		StoreFront: output.Account.StoreFront,
	}, nil
}

func (a *profileAppStoreAdapter) Revoke() error {
	return a.inner.Revoke()
}

func (a *profileAppStoreAdapter) AccountInfo() (AccountInfoResult, error) {
	out, err := a.inner.AccountInfo()
	if err != nil {
		return AccountInfoResult{}, err
	}
	// Cache full account for subsequent Lookup/Search/Download/Purchase calls.
	a.account = &out.Account
	return AccountInfoResult{
		Email:      out.Account.Email,
		Name:       out.Account.Name,
		StoreFront: out.Account.StoreFront,
	}, nil
}

func (a *profileAppStoreAdapter) Search(query string, limit int64) ([]AppInfo, error) {
	if a.account == nil {
		return nil, fmt.Errorf("AccountInfo must be called before Search")
	}
	out, err := a.inner.Search(ipaappstore.SearchInput{
		Account: *a.account,
		Term:    query,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}
	results := make([]AppInfo, len(out.Results))
	for i, app := range out.Results {
		results[i] = appToAppInfo(app)
	}
	return results, nil
}

func (a *profileAppStoreAdapter) Lookup(bundleID string) (AppInfo, error) {
	if a.account == nil {
		return AppInfo{}, fmt.Errorf("AccountInfo must be called before Lookup")
	}
	out, err := a.inner.Lookup(ipaappstore.LookupInput{
		Account:  *a.account,
		BundleID: bundleID,
	})
	if err != nil {
		return AppInfo{}, err
	}
	return appToAppInfo(out.App), nil
}

func (a *profileAppStoreAdapter) Download(input DownloadInput) (DownloadResult, error) {
	if a.account == nil {
		return DownloadResult{}, fmt.Errorf("AccountInfo must be called before Download")
	}
	var pb *progressbar.ProgressBar
	if input.Progress != nil {
		if w, ok := input.Progress.(*progressBarWrapper); ok {
			pb = w.inner
		}
	}
	out, err := a.inner.Download(ipaappstore.DownloadInput{
		Account:           *a.account,
		App:               appInfoToApp(input.BundleID, input.AppID),
		OutputPath:        input.OutputPath,
		Progress:          pb,
		ExternalVersionID: input.ExternalVersionID,
	})
	if err != nil {
		return DownloadResult{}, mapDownloadError(err)
	}
	return DownloadResult{
		DestinationPath: out.DestinationPath,
		Version:         extractVersionFromPath(out.DestinationPath),
		Sinfs:           sinfsToOur(out.Sinfs),
	}, nil
}

func (a *profileAppStoreAdapter) ReplicateSinf(sinfs []Sinf, packagePath string) error {
	ipaSinfs := make([]ipaappstore.Sinf, len(sinfs))
	for i, s := range sinfs {
		ipaSinfs[i] = ipaappstore.Sinf{ID: s.ID, Data: s.Data}
	}
	return a.inner.ReplicateSinf(ipaappstore.ReplicateSinfInput{
		Sinfs:       ipaSinfs,
		PackagePath: packagePath,
	})
}

func (a *profileAppStoreAdapter) Purchase(bundleID string, appID int64, price float64) error {
	if a.account == nil {
		return fmt.Errorf("AccountInfo must be called before Purchase")
	}
	return a.inner.Purchase(ipaappstore.PurchaseInput{
		Account: *a.account,
		App:     ipaappstore.App{ID: appID, BundleID: bundleID, Price: price},
	})
}

func (a *profileAppStoreAdapter) RefreshSession() error {
	if a.account == nil {
		return fmt.Errorf("AccountInfo must be called before RefreshSession")
	}
	bag, err := a.inner.Bag(ipaappstore.BagInput{})
	if err != nil {
		return fmt.Errorf("failed to get auth endpoint for re-login: %w", err)
	}
	output, err := a.inner.Login(ipaappstore.LoginInput{
		Email:    a.account.Email,
		Password: a.account.Password,
		Endpoint: bag.AuthEndpoint,
	})
	if err != nil {
		return err
	}
	a.account = &output.Account // update cached account with fresh token
	return nil
}

// NewProfileAppStore constructs a ProfileAppStore scoped to a single account
// profile with isolated keychain namespace and cookie jar (design DD-01).
//
// ipatool types are confined to this function and the adapter struct.
// Callers receive ProfileAppStore — our interface, not ipatool's.
func NewProfileAppStore(p account.Profile, configRoot string) (ProfileAppStore, error) {
	// 1. Keyring backend (macOS Keychain only for v1, design DD-02).
	ring, err := keyringOpener(keyring.Config{
		AllowedBackends: []keyring.BackendType{keyring.KeychainBackend},
		ServiceName:     KeychainServiceName,
		FileDir:         filepath.Join(configRoot, "keychain"),
		FilePasswordFunc: func(prompt string) (string, error) {
			return "", fmt.Errorf("file keyring backend is not supported in ipa-manager v1; only macOS Keychain is allowed")
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open keyring: %w", err)
	}

	// 2. Base ipatool keychain.
	kc := ipakeychain.New(ipakeychain.Args{Keyring: ring})

	// 3. ProfileKeychain namespace wrapper (ADR 0002).
	pkc := account.ProfileKeychain{Base: kc, ProfileID: p.ID}

	// 4. Per-profile persistent cookie jar.
	jarPath := account.CookieJarPath(p.ID, configRoot)
	if err := os.MkdirAll(filepath.Dir(jarPath), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create cookie jar directory: %w", err)
	}
	jar, err := cookiejar.New(&cookiejar.Options{Filename: jarPath})
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	// 5. OS + Machine (shared, no per-profile state).
	osys := operatingsystem.New()
	mac := machine.New(machine.Args{OS: osys})

	// 6. Construct ipatool AppStore with isolated deps, wrap in adapter.
	inner := ipaappstore.NewAppStore(ipaappstore.Args{
		Keychain:        pkc,
		CookieJar:       jar,
		OperatingSystem: osys,
		Machine:         mac,
	})

	return &profileAppStoreAdapter{inner: inner}, nil
}
