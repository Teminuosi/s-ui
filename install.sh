#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

cur_dir=$(pwd)

# Full-auto install mode. Enabled by SUI_AUTO=1 (or y). In auto mode every
# interactive prompt takes a sensible default: keep existing settings on an
# upgrade; on a fresh install generate random admin credentials and a random
# panel path, then print the access info. Example:
#   SUI_AUTO=1 bash <(curl -Ls https://raw.githubusercontent.com/Teminuosi/s-ui/main/install.sh)
SUI_AUTO="${SUI_AUTO:-}"

is_auto() {
    [[ "$SUI_AUTO" == "1" || "$SUI_AUTO" == "y" || "$SUI_AUTO" == "Y" ]]
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
    local port="$1"
    if command -v ss > /dev/null 2>&1; then
        ss -ltn 2> /dev/null | awk -v p=":${port}\$" '$4 ~ p {exit 0} END {exit 1}'
        return
    fi
    if command -v netstat > /dev/null 2>&1; then
        netstat -lnt 2> /dev/null | awk -v p=":${port} " '$4 ~ p {exit 0} END {exit 1}'
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

# check root
[[ $EUID -ne 0 ]] && echo -e "${red}Fatal error: ${plain} Please run this script with root privilege \n " && exit 1

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
            echo -e "this is a fresh installation,will generate random login info for security concerns:"
            echo -e "###############################################"
            echo -e "${green}username:${usernameTemp}${plain}"
            echo -e "${green}password:${passwordTemp}${plain}"
            echo -e "###############################################"
            echo -e "${red}if you forgot your login info,you can type ${green}s-ui${red} for configuration menu${plain}"
            /usr/local/s-ui/sui admin -username ${usernameTemp} -password ${passwordTemp}
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
        wget -N --no-check-certificate -O /tmp/s-ui-linux-$(arch).tar.gz https://github.com/Teminuosi/s-ui/releases/download/${last_version}/s-ui-linux-$(arch).tar.gz
        if [[ $? -ne 0 ]]; then
            echo -e "${red}Downloading s-ui failed, please be sure that your server can access Github ${plain}"
            exit 1
        fi
    else
        last_version=$1
        url="https://github.com/Teminuosi/s-ui/releases/download/${last_version}/s-ui-linux-$(arch).tar.gz"
        echo -e "Beginning the install s-ui v$1"
        wget -N --no-check-certificate -O /tmp/s-ui-linux-$(arch).tar.gz ${url}
        if [[ $? -ne 0 ]]; then
            echo -e "${red}download s-ui v$1 failed,please check the version exists${plain}"
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
