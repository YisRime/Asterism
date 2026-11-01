package provider

import (
	"slices"
	"strings"

	asterism "asterism/internal"
)

// 源注册表
var Registry = []asterism.Source{
	{Name: "brew_cask", Label: "Homebrew cask", Command: "brew install --cask {id}", Default: true, Parse: ParseCask},
	{Name: "brew_formula", Label: "Homebrew", Command: "brew install {id}", Default: true, Parse: ParseFormula},
	{Name: "macports", Label: "MacPorts", Command: "sudo port install {id}", Default: true, Parse: ParseMacPorts},
	{Name: "chocolatey", Label: "Chocolatey", Command: "choco install {id}", Default: true, Parse: ParseChocolatey},
	{Name: "winget", Label: "winget", Command: "winget install --id {id} -e", Default: true, Parse: ParseWinget},
	{Name: "scoop", Label: "Scoop", Command: "scoop install {id}", Default: true, Parse: ParseScoop},
	{Name: "flathub", Label: "Flathub", Command: "flatpak install flathub {id}", Default: true, Parse: ParseFlathub},
	{Name: "snap", Label: "Snap", Command: "sudo snap install {id}", Default: true, Parse: ParseSnap},
	{Name: "aur", Label: "AUR", Command: "yay -S {id}", Default: true, Parse: ParseAUR},
	{Name: "arch", Label: "Arch Linux", Command: "sudo pacman -S {id}", Default: true, Parse: ParseArch},
	{Name: "nix", Label: "Nixpkgs", Command: "nix profile install nixpkgs#{id}", Default: true, Parse: ParseNix},
	{Name: "alpine", Label: "Alpine", Command: "sudo apk add {id}", Default: true, Parse: ParseAlpine},
	{Name: "debian", Label: "Debian", Command: "sudo apt install {id}", Default: true, Parse: ParseDebian},
	{Name: "opensuse", Label: "openSUSE", Command: "sudo zypper install {id}", Default: true, Parse: ParseOpenSUSE},
	{Name: "freebsd", Label: "FreeBSD", Command: "sudo pkg install {id}", Default: true, Parse: ParseFreeBSD},
	{Name: "fdroid", Label: "F-Droid", InstallURL: "https://f-droid.org/packages/{id}/", Default: true,
		Parse: func() ([]*asterism.Package, error) {
			return ParseFdroid("fdroid", "https://f-droid.org/repo/index-v1.json")
		}},
	{Name: "izzy", Label: "IzzyOnDroid", InstallURL: "https://apt.izzysoft.de/fdroid/index/apk/{id}", Default: true,
		Parse: func() ([]*asterism.Package, error) {
			return ParseFdroid("izzy", "https://apt.izzysoft.de/fdroid/repo/index-v1.json")
		}},
}

// 仓库分片
type repoItem struct{ Name, URL string }

// 多仓抓取
func collectRepos(repos []repoItem,
	load func(address, repo string) ([]*asterism.Package, error)) ([]*asterism.Package, error) {
	var out []*asterism.Package
	for _, repo := range repos {
		records, err := load(repo.URL, repo.Name)
		if err != nil {
			continue
		}
		out = append(out, records...)
	}
	out = uniquePackages(out)
	if len(out) == 0 {
		return nil, asterism.ErrFailed
	}
	return out, nil
}

// 去重记录
func uniquePackages(records []*asterism.Package) []*asterism.Package {
	seen := make(map[string]bool, len(records))
	unique := records[:0]
	for _, record := range records {
		if seen[record.Origin] {
			continue
		}
		seen[record.Origin] = true
		unique = append(unique, record)
	}
	return unique
}

// 并发分页
func fetchPages(pages, parallel int, load func(index int) ([]*asterism.Package, error)) [][]*asterism.Package {
	indexes := make([]int, pages)
	for index := range indexes {
		indexes[index] = index
	}
	return asterism.Parallel(indexes, parallel, func(index int) []*asterism.Package {
		records, _ := load(index)
		return records
	})
}

// 包名过滤
func excludedPackage(name string, prefixes, suffixes []string, libExceptions bool) bool {
	lower := strings.ToLower(name)
	library := strings.HasPrefix(lower, "lib")
	exempted := library && libExceptions && slices.ContainsFunc(
		[]string{"libreoffice", "librewolf", "librecad", "librepcb", "libresprite"},
		func(prefix string) bool { return strings.HasPrefix(lower, prefix) })
	if library && libExceptions && !exempted {
		return true
	}
	if slices.ContainsFunc(prefixes, func(prefix string) bool {
		return !(prefix == "lib" && exempted) && strings.HasPrefix(lower, prefix)
	}) {
		return true
	}
	return slices.ContainsFunc(suffixes, func(suffix string) bool { return strings.HasSuffix(lower, suffix) })
}
