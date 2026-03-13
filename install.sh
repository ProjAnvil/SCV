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

SKILL_LANG="en"

for arg in "$@"; do
    case $arg in
        -h|--help)
        echo "Usage: $0 [--lang=en|zh-cn]"
        echo ""
        echo "Options:"
        echo "  --lang=en      Install English skill (default)"
        echo "  --lang=zh-cn   Install Chinese skill"
        echo "  -h, --help     Show this help message"
        exit 0
        ;;
        --lang=*)
            SKILL_LANG="${arg#*=}"
            ;;
    esac
done

print_info "SCV Installation Script"
print_info "OS Detected: $OS_TYPE"
print_info "Language: $SKILL_LANG"
echo ""

# Windows-specific warnings
if [ "$OS_TYPE" = "windows" ]; then
    print_warning "Windows Detected"
    print_info "On Windows, files will be copied instead of linked."
    print_info "You'll need to re-run this script after making changes to skills."
    echo ""
fi

print_info "Step 1: Creating SCV configuration directory..."
mkdir -p ~/.scv
mkdir -p ~/.scv/repos
mkdir -p ~/.scv/analysis
print_success "Created ~/.scv directory structure"
echo ""

print_info "Step 2: Copying configuration file..."
if [ -f ~/.scv/config.json ]; then
    print_warning "config.json already exists. Skipping..."
else
    cp "$SCRIPT_DIR/config.example.json" ~/.scv/config.json
    print_success "Copied config.example.json to ~/.scv/config.json"
fi
echo ""

print_info "Step 3: Installing SCV skill ($SKILL_LANG)..."

if [ -d "$HOME/.claude" ]; then
    # Remove old commands if they exist
    if [ -d "$HOME/.claude/commands" ]; then
        if ls "$HOME/.claude/commands"/scv.* 1> /dev/null 2>&1; then
            print_warning "Removing old scv.* commands..."
            rm -f "$HOME/.claude/commands"/scv.*
        fi
    fi

    # Remove old skill structure (skills/scv) if exists
    if [ -d "$HOME/.claude/skills/scv" ]; then
        print_warning "Removing old skill structure..."
        rm -rf "$HOME/.claude/skills/scv"
    fi

    # Check if language-specific skill exists
    if [ ! -d "$SCRIPT_DIR/skills/$SKILL_LANG" ]; then
        print_error "Skill not found: $SCRIPT_DIR/skills/$SKILL_LANG"
        print_info "Available languages: en, zh-cn"
        exit 1
    fi

    # Install language-specific skill as 'scv'
    mkdir -p "$HOME/.claude/skills"
    cp -r "$SCRIPT_DIR/skills/$SKILL_LANG" "$HOME/.claude/skills/scv"
    print_success "Installed scv skill ($SKILL_LANG) to ~/.claude/skills/scv"
else
    print_error "Claude directory not found at ~/.claude"
    print_info "Please make sure Claude Code is installed first"
    exit 1
fi
echo ""

print_info "Step 4: Installing project-analyzer agent ($SKILL_LANG)..."
mkdir -p "$HOME/.claude/agents"
if [ -f "$SCRIPT_DIR/agents/$SKILL_LANG/project-analyzer.md" ]; then
    cp "$SCRIPT_DIR/agents/$SKILL_LANG/project-analyzer.md" "$HOME/.claude/agents/project-analyzer.md"
    print_success "Installed project-analyzer agent ($SKILL_LANG) to ~/.claude/agents/"
else
    print_error "Agent definition not found: $SCRIPT_DIR/agents/$SKILL_LANG/project-analyzer.md"
    print_info "Available languages: en, zh-cn"
    exit 1
fi
echo ""

print_success "SCV installation completed!"
echo ""
echo "Installation summary:"
echo "  - Configuration: ~/.scv/config.json"
echo "  - Repository storage: ~/.scv/repos/"
echo "  - Analysis output: ~/.scv/analysis/"
echo "  - Skill: ~/.claude/skills/scv ($SKILL_LANG)"
echo "  - Agent: ~/.claude/agents/project-analyzer.md"
echo ""
print_info "You can now use SCV commands in Claude Code:"
echo "  /scv run <path|url>  - Analyze a single repository"
echo "  /scv batchRun        - Batch analyze multiple repositories (parallel)"
echo "  /scv gather <opts>   - Clone and manage repositories"
echo ""
if [ "$OS_TYPE" = "windows" ]; then
    print_warning "Windows Notes:"
    echo "  - Files are copied, not linked"
    echo "  - Re-run this script after updating skills"
    echo "  - To remove: rm -rf ~/.scv ~/.claude/skills/scv ~/.claude/agents/project-analyzer.md"
else
    print_info "To switch language, run: $0 --lang=<en|zh-cn>"
fi
echo ""
print_info "For more information, see README.md"
