# Asterism

<div align="center">

[![GitHub stars](https://img.shields.io/github/stars/YisRime/Asterism?style=flat-square)](https://github.com/YisRime/Asterism/stargazers)
[![Go version](https://img.shields.io/github/go-mod/go-version/YisRime/Asterism?style=flat-square)](go.mod)

**小众宝藏软件索引**

如果这个项目对您有帮助，请给我一个 ⭐️ Star！

**QQ 群**：[855571375](https://qm.qq.com/q/PdLMx9Jowq) - 用户交流、问题反馈

</div>

---

Asterism 是一份索引：把同一款软件在不同软件源里的档案对齐成一条记录——哪些平台能装、各自的命令是什么、谁还在维护。

排序不看热度，看跨源覆盖：一款软件被 Homebrew、Debian、Flathub 各自收录，说明三个互不相干的团队都认为它值得打包，这比 star 数难刷得多。热度榜只推头部，这样更容易捞出没人宣传、却确实好用的小众软件。

## 开始

```bash
go build -o Asterism.exe ./cmd/asterism
./Asterism.exe
```

| 参数 | 默认 | 说明 |
|---|---|---|
| `-port` | `8720` | 监听端口；`0` 则不监听 |
| `-source` | 空 | 抓取源名；默认全部 |
| `-token` | 空 | GitHub Token；备选 Shields 徽章 |

## 数据源

| 源 | 平台 | 安装命令 |
|---|---|---|
| Homebrew cask | macOS | `brew install --cask 包名` |
| Homebrew formula | macOS / Linux | `brew install 包名` |
| MacPorts | macOS | `sudo port install 包名` |
| Chocolatey | Windows | `choco install 包名` |
| winget | Windows | `winget install --id 包名 -e` |
| Scoop | Windows | `scoop install 包名` |
| Flathub | Linux | `flatpak install flathub ID` |
| Snap | Linux | `sudo snap install 包名` |
| AUR | Linux | `yay -S 包名` |
| Arch Linux | Linux | `sudo pacman -S 包名` |
| Nixpkgs | Linux / macOS | `nix profile install nixpkgs#属性` |
| Alpine | Linux | `sudo apk add 包名` |
| Debian | Linux | `sudo apt install 包名` |
| openSUSE | Linux | `sudo zypper install 包名` |
| FreeBSD | FreeBSD | `sudo pkg install 包名` |
| F-Droid | Android | `https://f-droid.org/packages/{id}/` |
| IzzyOnDroid | Android | `https://apt.izzysoft.de/fdroid/index/apk/{id}` |
