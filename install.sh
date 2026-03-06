#!/bin/bash

# SCV Installation Script - Installs SCV (Source Code Vault) for Claude Code
# Supports Windows (Git Bash, MSYS2, Cygwin), macOS, and Linux

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Detect OS
detect_os() {
    if [[ "$OSTYPE" == "msys" ]] || [[ "$OSTYPE" == "win32" ]] || [[ "$OSTYPE" == "cygwin" ]]; then
        echo "windows"
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        echo "macos"
    else
        echo "linux"
    fi
}

OS_TYPE=$(detect_os)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ ! -f "$SCRIPT_DIR/config.example.json" ]; then
    print_error "Please run this script from the SCV project root directory"
    exit 1
fi

PROMPT_LANG="en"

for arg in "$@"; do
    case $arg in
        -h|--help)
        echo "Usage: $0 [--lang=en|zh-cn]"
        echo ""
        echo "Options:"
        echo "  --lang=en      Install with English prompts (default)"
        echo "  --lang=zh-cn   Install with Chinese prompts"
        echo "  -h, --help     Show this help message"
        exit 0
        ;;
        --lang=*)
            PROMPT_LANG="${arg#*=}"
            ;;
    esac
done

print_info "SCV Installation Script"
print_info "OS Detected: $OS_TYPE"
print_info "Language: $PROMPT_LANG"
echo ""

# Windows-specific warnings
if [ "$OS_TYPE" = "windows" ]; then
    print_warning "Windows Detected"
    print_info "On Windows, files will be copied instead of linked."
    print_info "You'll need to re-run this script after making changes to prompts/commands."
    echo ""
fi

# Function to copy directory or file (used for Windows compatibility)
copy_item() {
    local source=$1
    local target=$2
    local item_type=$3  # "dir" or "file"

    if [ "$item_type" = "dir" ]; then
        cp -r "$source" "$target"
    else
        cp "$source" "$target"
    fi
}

print_info "Step 1: Creating SCV configuration directory..."
mkdir -p ~/.scv
print_success "Created ~/.scv directory"
echo ""

print_info "Step 2: Copying configuration file..."
if [ -f ~/.scv/config.json ]; then
    print_warning "config.json already exists. Skipping..."
else
    cp "$SCRIPT_DIR/config.example.json" ~/.scv/config.json
    print_success "Copied config.example.json to ~/.scv/config.json"
fi
echo ""

print_info "Step 3: Copying prompts ($PROMPT_LANG)..."

if [ -d ~/.scv/prompts ]; then
    print_warning "Removing existing ~/.scv/prompts directory..."
    rm -rf ~/.scv/prompts
fi

if [ -d "$SCRIPT_DIR/prompts/$PROMPT_LANG" ]; then
    cp -r "$SCRIPT_DIR/prompts/$PROMPT_LANG" ~/.scv/prompts
    print_success "Copied prompts/$PROMPT_LANG to ~/.scv/prompts"
else
    print_error "Prompts directory not found: $SCRIPT_DIR/prompts/$PROMPT_LANG"
    exit 1
fi
echo ""

print_info "Step 4: Copying commands to Claude directory..."

if [ -d "$HOME/.claude" ]; then
    mkdir -p "$HOME/.claude/commands"

    # Remove only scv.* commands to avoid affecting other commands
    if ls "$HOME/.claude/commands"/scv.* 1> /dev/null 2>&1; then
        print_warning "Removing existing scv.* commands..."
        rm -f "$HOME/.claude/commands"/scv.*
    fi

    cp "$SCRIPT_DIR/commands"/scv.* "$HOME/.claude/commands/"
    print_success "Copied scv commands to ~/.claude/commands"
else
    print_error "Claude directory not found at ~/.claude"
    print_info "Please make sure Claude Code is installed first"
    exit 1
fi
echo ""

print_success "SCV installation completed!"
echo ""
echo "Installation summary:"
echo "  - Configuration: ~/.scv/config.json"
echo "  - Prompts: ~/.scv/prompts ($PROMPT_LANG)"
echo "  - Commands: ~/.claude/commands"
echo ""
print_info "You can now use SCV commands in Claude Code:"
echo "  /scv.gather   - Clone and manage repositories"
echo "  /scv.run      - Analyze a single codebase"
echo "  /scv.batchRun  - Batch analyze multiple repositories"
echo ""
if [ "$OS_TYPE" = "windows" ]; then
    print_warning "Windows Notes:"
    echo "  - Files are copied, not linked"
    echo "  - Re-run this script after updating prompts or commands"
    echo "  - To remove: rm -rf ~/.scv/prompts ~/.claude/commands"
else
    print_info "To switch language, run: $0 --lang=<en|zh-cn>"
fi
echo ""
print_info "For more information, see README.md"
