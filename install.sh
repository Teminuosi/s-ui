#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

cur_dir=$(pwd)

# Full-auto is the DEFAULT install mode: every command runs start-to-finish with
# no prompts. On a fresh install it generates random admin credentials + a random
# panel path + free ports + an API token and opens the firewall, then prints the
# access info; on an upgrade it keeps existing settings untouched. To get the old
# interactive flow back, set SUI_AUTO=0 (or n). Examples:
#   bash <(curl -Ls https://raw.githubusercontent.com/Teminuosi/s-ui/main/install.sh)            # auto (default)
#   SUI_AUTO=0 bash <(curl -Ls https://raw.githubusercontent.com/Teminuosi/s-ui/main/install.sh) # interactive
SUI_AUTO="${SUI_AUTO:-1}"

is_auto() {
    [[ "$SUI_AUTO" != "0" && "$SUI_AUTO" != "n" && "$SUI_AUTO" != "N" ]]
}

# auto_read VAR DEFAULT PROMPT
# In auto mode: assign DEFAULT to VAR (caller scope) and echo the choice.
# Otherwise behave like the plain `read -rp PROMPT VAR` it replaces.
auto_read() {
    local __av="$1" __ad="$2" __ap="$3"
    if is_auto; then
        printf -v "$__av" '%s' "$__ad"
        echo -e "${yellow}[auto]${plain} ${__ap}${__ad}"
    else
        read -rp "$__ap" "$__av"
    fi
}

# Alphanumeric random string (URL-safe, no base64 specials).
gen_random_string() {
    local length="$1"
    LC_ALL=C tr -dc 'a-zA-Z0-9' < /dev/urandom 2>/dev/null | head -c "$length"
}

# Is a TCP port currently being listened on? (used to avoid clashing with an
# existing panel such as 3x-ui, whose default sub port is also 2096.)
is_port_in_use() {
    # 整行匹配「:端口 + 空白/行尾」,不按第几列取 —— 不同版本 ss/netstat 列数不一样,
    # 按列取会悄悄失效,而失效表现是「误判端口空闲」、装完照样起不来,比报错更难查。
    # 末尾的边界防止 12096 / 20960 被误判成 2096。
    local port="$1"
    if command -v ss > /dev/null 2>&1; then
        ss -ltn 2> /dev/null | grep -qE "[:.]${port}([[:space:]]|$)"
        return
    fi
    if command -v netstat > /dev/null 2>&1; then
        netstat -lnt 2> /dev/null | grep -qE "[:.]${port}([[:space:]]|$)"
        return
    fi
    if command -v lsof > /dev/null 2>&1; then
        lsof -nP -iTCP:"${port}" -sTCP:LISTEN > /dev/null 2>&1 && return 0
    fi
    return 1
}

# Echo a free port: prefer $1, else fall back to a random high port. Keeps the
# nice default when it's free, only deviates when something already holds it.
pick_port() {
    local preferred="$1" p
    if ! is_port_in_use "$preferred"; then
        echo "$preferred"
        return
    fi
    for _ in $(seq 1 30); do
        p=$(shuf -i 20000-60000 -n 1)
        if ! is_port_in_use "$p"; then
            echo "$p"
            return
        fi
    done
    echo "$preferred"
}

# On upgrade s-ui was just stopped, so anything still holding our panel/sub port
# is a DIFFERENT process (classically a 3x-ui install also defaulting to sub port
# 2096). Left alone, s-ui crash-loops on "bind: address already in use" and the
# panel 503s. Detect the clash and migrate the affected port to a free one.
resolve_port_clash() {
    local cur_port cur_sub params=""
    cur_port=$(/usr/local/s-ui/sui setting -show 2>/dev/null | grep -i "Panel port:" | grep -oE '[0-9]+' | head -1)
    cur_sub=$(/usr/local/s-ui/sui setting -show 2>/dev/null | grep -i "Sub port:" | grep -oE '[0-9]+' | head -1)
    if [[ -n "$cur_port" ]] && is_port_in_use "$cur_port"; then
        local np=$(pick_port "$cur_port")
        params="$params -port $np"
        echo -e "${red}[auto] Panel port ${cur_port} is already in use by another process; moving the panel to ${np}.${plain}"
    fi
    if [[ -n "$cur_sub" ]] && is_port_in_use "$cur_sub"; then
        local ns=$(pick_port "$cur_sub")
        params="$params -subPort $ns"
        echo -e "${red}[auto] Sub port ${cur_sub} is already in use (another panel? e.g. 3x-ui on 2096); moving subscriptions to ${ns}.${plain}"
    fi
    if [[ -n "$params" ]]; then
        /usr/local/s-ui/sui setting ${params}
        echo -e "${yellow}[auto] Ports changed to avoid a clash. Update any subscription links / bookmarks accordingly.${plain}"
    fi
}

# check root
[[ $EUID -ne 0 ]] && echo -e "${red}Fatal error: ${plain} Please run this script with root privilege \n " && exit 1

# 一键彻底清除入口: bash <(curl -Ls .../install.sh) purge
# 为什么不复用 s-ui.sh 的卸载:安装被中断时 /usr/bin/s-ui 可能压根没铺下去,
# 那条路走不通。这里只依赖 root,不依赖任何已安装的文件,也不做系统检测 ——
# 检测失败就 exit 的话,最需要清理的机器反而清不了。
if [[ "$1" == "purge" || "$1" == "uninstall" || "$1" == "--purge" ]]; then
    echo -e "${yellow}Removing every trace of s-ui from this system...${plain}"

    # 每一步独立执行、失败不影响后面:装坏的机器上服务和文件往往只存在一部分。
    systemctl stop s-ui >/dev/null 2>&1
    systemctl disable s-ui >/dev/null 2>&1
    # 二进制被删掉后残留的进程会占着端口,先杀干净
    pkill -9 -f '/usr/local/s-ui' >/dev/null 2>&1
    rm -f /etc/systemd/system/s-ui.service
    rm -f /etc/systemd/system/multi-user.target.wants/s-ui.service
    systemctl daemon-reload >/dev/null 2>&1
    systemctl reset-failed >/dev/null 2>&1

    rm -rf /etc/s-ui/
    rm -rf /usr/local/s-ui/
    rm -f /usr/bin/s-ui

    echo -e "${green}Done. s-ui has been completely removed.${plain}"
    echo -e "Reinstall with: ${green}bash <(curl -Ls https://raw.githubusercontent.com/Teminuosi/s-ui/main/install.sh)${plain}"
    exit 0
fi


# Check OS and set release variable
if [[ -f /etc/os-release ]]; then
    source /etc/os-release
    release=$ID
elif [[ -f /usr/lib/os-release ]]; then
    source /usr/lib/os-release
    release=$ID
else
    echo "Failed to check the system OS, please contact the author!" >&2
    exit 1
fi
echo "The OS release is: $release"

arch() {
    case "$(uname -m)" in
    x86_64 | x64 | amd64) echo 'amd64' ;;
    i*86 | x86) echo '386' ;;
    armv8* | armv8 | arm64 | aarch64) echo 'arm64' ;;
    armv7* | armv7 | arm) echo 'armv7' ;;
    armv6* | armv6) echo 'armv6' ;;
    armv5* | armv5) echo 'armv5' ;;
    s390x) echo 's390x' ;;
    *) echo -e "${green}Unsupported CPU architecture! ${plain}" && rm -f install.sh && exit 1 ;;
    esac
}

echo "arch: $(arch)"

install_base() {
    case "${release}" in
    centos | almalinux | rocky | oracle)
        yum -y update && yum install -y -q wget curl tar tzdata
        ;;
    fedora)
        dnf -y update && dnf install -y -q wget curl tar tzdata
        ;;
    arch | manjaro | parch)
        pacman -Syu && pacman -Syu --noconfirm wget curl tar tzdata
        ;;
    opensuse-tumbleweed)
        zypper refresh && zypper -q install -y wget curl tar timezone
        ;;
    *)
        apt-get update && apt-get install -y -q wget curl tar tzdata
        ;;
    esac
}

config_after_install() {
    echo -e "${yellow}Migration... ${plain}"
    /usr/local/s-ui/sui migrate

    # Full-auto mode: no prompts. Fresh install -> random credentials + random
    # panel path; upgrade -> keep existing settings untouched.
    if is_auto; then
        if [[ ! -f "/usr/local/s-ui/db/s-ui.db" ]]; then
            local config_account=$(gen_random_string 10)
            local config_password=$(gen_random_string 12)
            local config_path=$(gen_random_string 15)
            # Pick free ports so we don't clash with an existing panel (e.g. a
            # 3x-ui install already on 2096). Keep the defaults when they're free.
            local config_port=$(pick_port 2095)
            local config_subPort=$(pick_port 2096)
            echo -e "${yellow}[auto] Fresh install: generating random credentials, panel path and free ports...${plain}"
            [[ "$config_port" != "2095" ]] && echo -e "${yellow}[auto] Port 2095 busy, using ${config_port} for the panel.${plain}"
            [[ "$config_subPort" != "2096" ]] && echo -e "${yellow}[auto] Port 2096 busy, using ${config_subPort} for subscriptions.${plain}"
            /usr/local/s-ui/sui setting -port "${config_port}" -path "/${config_path}/" -subPort "${config_subPort}"
            /usr/local/s-ui/sui admin -username "${config_account}" -password "${config_password}"
            # Generate an APIv2 token so this server can be managed from another
            # panel out of the box (central management).
            local config_token=$(/usr/local/s-ui/sui token -desc install 2>/dev/null)
            echo -e "###############################################"
            echo -e "${green}username:${config_account}${plain}"
            echo -e "${green}password:${config_password}${plain}"
            echo -e "${green}panel port:${config_port}${plain}"
            echo -e "${green}panel path:/${config_path}/${plain}"
            echo -e "${green}sub port:${config_subPort}${plain}"
            if [[ -n "$config_token" ]]; then
                echo -e "${green}API token (copy the line below):${plain}"
                echo -e "${config_token}"
            fi
            echo -e "###############################################"
            echo -e "${red}If you forget your login info, type ${green}s-ui${red} on the server for the menu.${plain}"
        else
            echo -e "${yellow}[auto] Upgrade detected: keeping existing settings.${plain}"
            # Guard against a port clash with another panel on the same box.
            resolve_port_clash
            local up_token=$(/usr/local/s-ui/sui token -desc upgrade 2>/dev/null)
            echo -e "###############################################"
            /usr/local/s-ui/sui admin -show 2>/dev/null
            if [[ -n "$up_token" ]]; then
                echo -e "${green}API token (copy the line below):${plain}"
                echo -e "${up_token}"
            fi
            echo -e "${red}If you forget your login info, type ${green}s-ui${red} on the server for the menu.${plain}"
            echo -e "###############################################"
        fi
        return
    fi

    echo -e "${yellow}Install/update finished! For security it's recommended to modify panel settings ${plain}"
    read -p "Do you want to continue with the modification [y/n]? ": config_confirm
    if [[ "${config_confirm}" == "y" || "${config_confirm}" == "Y" ]]; then
        echo -e "Enter the ${yellow}panel port${plain} (leave blank for existing/default value):"
        read config_port
        echo -e "Enter the ${yellow}panel path${plain} (leave blank for existing/default value):"
        read config_path

        # Sub configuration
        echo -e "Enter the ${yellow}subscription port${plain} (leave blank for existing/default value):"
        read config_subPort
        echo -e "Enter the ${yellow}subscription path${plain} (leave blank for existing/default value):" 
        read config_subPath

        # Set configs
        echo -e "${yellow}Initializing, please wait...${plain}"
        params=""
        [ -z "$config_port" ] || params="$params -port $config_port"
        [ -z "$config_path" ] || params="$params -path $config_path"
        [ -z "$config_subPort" ] || params="$params -subPort $config_subPort"
        [ -z "$config_subPath" ] || params="$params -subPath $config_subPath"
        /usr/local/s-ui/sui setting ${params}

        read -p "Do you want to change admin credentials [y/n]? ": admin_confirm
        if [[ "${admin_confirm}" == "y" || "${admin_confirm}" == "Y" ]]; then
            # First admin credentials
            read -p "Please set up your username:" config_account
            read -p "Please set up your password:" config_password

            # Set credentials
            echo -e "${yellow}Initializing, please wait...${plain}"
            /usr/local/s-ui/sui admin -username ${config_account} -password ${config_password}
        else
            echo -e "${yellow}Your current admin credentials: ${plain}"
            /usr/local/s-ui/sui admin -show
        fi
    else
        echo -e "${red}cancel...${plain}"
        if [[ ! -f "/usr/local/s-ui/db/s-ui.db" ]]; then
            local usernameTemp=$(head -c 6 /dev/urandom | base64)
            local passwordTemp=$(head -c 6 /dev/urandom | base64)
            /usr/local/s-ui/sui admin -username ${usernameTemp} -password ${passwordTemp}
            # Mint an APIv2 token even on a plain (non-auto) fresh install, so this
            # panel can be managed centrally out of the box. (qs31)
            local config_token=$(/usr/local/s-ui/sui token -desc install 2>/dev/null)
            echo -e "this is a fresh installation,will generate random login info for security concerns:"
            echo -e "###############################################"
            echo -e "${green}username:${usernameTemp}${plain}"
            echo -e "${green}password:${passwordTemp}${plain}"
            if [[ -n "$config_token" ]]; then
                echo -e "${green}API token (copy the line below):${plain}"
                echo -e "${config_token}"
            fi
            echo -e "###############################################"
            echo -e "${red}if you forgot your login info,you can type ${green}s-ui${red} for configuration menu${plain}"
        else
            echo -e "${red} this is your upgrade,will keep old settings,if you forgot your login info,you can type ${green}s-ui${red} for configuration menu${plain}"
        fi
    fi
}

# Best-effort: open the panel/sub ports and the node port range in the host
# firewall so nodes are reachable out of the box. Full-auto only.
open_firewall() {
    is_auto || return 0
    local panel_port sub_port
    panel_port=$(/usr/local/s-ui/sui setting -show 2>/dev/null | grep -i "Panel port:" | grep -oE '[0-9]+' | head -1)
    sub_port=$(/usr/local/s-ui/sui setting -show 2>/dev/null | grep -i "Sub port:" | grep -oE '[0-9]+' | head -1)
    if command -v ufw > /dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
        [[ -n "$panel_port" ]] && ufw allow "${panel_port}/tcp" > /dev/null 2>&1
        [[ -n "$sub_port" ]] && ufw allow "${sub_port}/tcp" > /dev/null 2>&1
        ufw allow 10000:60000/tcp > /dev/null 2>&1
        ufw allow 10000:60000/udp > /dev/null 2>&1
        ufw reload > /dev/null 2>&1
        echo -e "${green}[auto] Firewall (ufw) opened: ${panel_port}, ${sub_port}, 10000-60000 tcp/udp.${plain}"
    elif command -v firewall-cmd > /dev/null 2>&1 && firewall-cmd --state > /dev/null 2>&1; then
        [[ -n "$panel_port" ]] && firewall-cmd --permanent --add-port="${panel_port}/tcp" > /dev/null 2>&1
        [[ -n "$sub_port" ]] && firewall-cmd --permanent --add-port="${sub_port}/tcp" > /dev/null 2>&1
        firewall-cmd --permanent --add-port=10000-60000/tcp > /dev/null 2>&1
        firewall-cmd --permanent --add-port=10000-60000/udp > /dev/null 2>&1
        firewall-cmd --reload > /dev/null 2>&1
        echo -e "${green}[auto] Firewall (firewalld) opened: ${panel_port}, ${sub_port}, 10000-60000 tcp/udp.${plain}"
    fi
}

prepare_services() {
    if [[ -f "/etc/systemd/system/sing-box.service" ]]; then
        echo -e "${yellow}Stopping sing-box service... ${plain}"
        systemctl stop sing-box
        rm -f /usr/local/s-ui/bin/sing-box /usr/local/s-ui/bin/runSingbox.sh /usr/local/s-ui/bin/signal
    fi
    if [[ -e "/usr/local/s-ui/bin" ]]; then
        echo -e "###############################################################"
        echo -e "${green}/usr/local/s-ui/bin${red} directory exists yet!"
        echo -e "Please check the content and delete it manually after migration ${plain}"
        echo -e "###############################################################"
    fi
    systemctl daemon-reload
}

# Download a GitHub release asset with retry + mirror fallback. A CN VPS often
# reaches github.com (so the version fetch above succeeds) but then gets its
# release-CDN connection reset mid-transfer -- the classic wget "Cannot write to
# '/tmp/...tar.gz' (Success)." followed by "Downloading s-ui failed". So we retry
# the direct URL, then fall back through public GitHub proxy mirrors, and verify
# the result is a real gzip each time so a mirror's HTML error page can't slip
# through as a "successful" download.
# Usage: download_release OUT_FILE GITHUB_URL
download_release() {
    local out="$1" gh_url="$2" src label
    local sources=(
        "$gh_url"
        "https://ghfast.top/$gh_url"
        "https://gh-proxy.com/$gh_url"
        "https://ghproxy.net/$gh_url"
    )
    for src in "${sources[@]}"; do
        if [[ "$src" == "$gh_url" ]]; then
            echo -e "${yellow}Downloading (direct github.com)...${plain}"
        else
            label="${src#https://}"; label="${label%%/*}"
            echo -e "${yellow}Direct download failed, retrying via mirror: ${label}${plain}"
        fi
        if wget --no-check-certificate --timeout=30 --tries=2 -O "$out" "$src" \
            && tar -tzf "$out" > /dev/null 2>&1; then
            return 0
        fi
        rm -f "$out"
    done
    return 1
}

install_s-ui() {
    cd /tmp/

    if [ $# == 0 ]; then
        last_version=$(curl -Ls "https://api.github.com/repos/Teminuosi/s-ui/releases/latest" | grep '"tag_name":' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
        # Fall back to the most recent release (covers prerelease-only repos:
        # /releases/latest skips prereleases, /releases lists everything).
        if [[ -z "$last_version" ]]; then
            last_version=$(curl -Ls "https://api.github.com/repos/Teminuosi/s-ui/releases" | grep '"tag_name":' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
        fi
        if [[ ! -n "$last_version" ]]; then
            echo -e "${red}Failed to fetch s-ui version, it maybe due to Github API restrictions, please try it later${plain}"
            exit 1
        fi
        echo -e "Got s-ui latest version: ${last_version}, beginning the installation..."
        if ! download_release /tmp/s-ui-linux-$(arch).tar.gz "https://github.com/Teminuosi/s-ui/releases/download/${last_version}/s-ui-linux-$(arch).tar.gz"; then
            echo -e "${red}Downloading s-ui failed after trying direct + mirrors. Make sure your server can reach Github (or a proxy) and that /tmp has free disk space.${plain}"
            exit 1
        fi
    else
        last_version=$1
        url="https://github.com/Teminuosi/s-ui/releases/download/${last_version}/s-ui-linux-$(arch).tar.gz"
        echo -e "Beginning the install s-ui $1"
        if ! download_release /tmp/s-ui-linux-$(arch).tar.gz "${url}"; then
            echo -e "${red}download s-ui $1 failed (tried direct + mirrors), please check the version exists${plain}"
            exit 1
        fi
    fi

    if [[ -e /usr/local/s-ui/ ]]; then
        systemctl stop s-ui
    fi

    tar zxvf s-ui-linux-$(arch).tar.gz
    rm s-ui-linux-$(arch).tar.gz -f

    chmod +x s-ui/sui s-ui/s-ui.sh
    cp s-ui/s-ui.sh /usr/bin/s-ui
    cp -rf s-ui /usr/local/
    cp -f s-ui/*.service /etc/systemd/system/
    rm -rf s-ui

    config_after_install
    open_firewall
    prepare_services

    systemctl enable s-ui --now

    echo -e "${green}s-ui ${last_version}${plain} installation finished, it is up and running now..."
    echo -e "You may access the Panel with following URL(s):${green}"
    /usr/local/s-ui/sui uri
    echo -e "${plain}"
    echo -e ""
    s-ui help
}

echo -e "${green}Executing...${plain}"
install_base
install_s-ui $1
