# Assets / 图标资源

## OpenCode logo

`logo.svg` is the OpenCode mark on a 512 × 512 dark canvas. `logo.png` is its
512 × 512 raster version, used by CPA management clients and the plugin store.
The READMEs use the SVG so the mark stays sharp at different display sizes.

Official brand resources: <https://opencode.ai/brand>.

Plugin icon URL:

```text
https://raw.githubusercontent.com/dillonzq/cpa-opencode/main/assets/logo.png
```

To regenerate the PNG after editing the SVG:

```sh
rsvg-convert -w 512 -h 512 assets/logo.svg -o assets/logo.png
```

`logo.svg` 为深色背景上的 OpenCode 标志；`logo.png` 为其 512 × 512 位图版本，
用于 CPA 管理界面和插件商店。中英文 README 使用 SVG。

OpenCode 的名称和标志归其各自权利人所有，用于标识本插件对接的服务；
本仓库的 MIT 许可证不授予相关商标权。

The OpenCode name and mark belong to their respective rights holders and
identify the service this plugin connects to. This repository's MIT license
does not grant trademark rights.
