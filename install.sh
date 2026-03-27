#!/bin/bash

# SCV Installation Script - Installs SCV (Source Code Vault) for Claude Code
# Supports Windows (Git Bash, MSYS2, Cygwin), macOS, and Linux

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

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

# Default language
SKILL_LANG="en"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --lang=*)
            SKILL_LANG="${1#*=}"
            shift
            ;;
        --lang)
            SKILL_LANG="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --lang=LANG, --lang LANG    Set language (en or zh-cn, default: en)"
            echo "  -h, --help                  Show this help message"
            echo ""
            echo "Examples:"
            echo "  $0                  # Use default language (en)"
            echo "  $0 --lang=zh-cn     # Use Chinese"
            echo "  $0 --lang en        # Use English"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Validate language code
if [[ "$SKILL_LANG" != "en" && "$SKILL_LANG" != "zh-cn" ]]; then
    echo -e "${RED}Error: Unsupported language '$SKILL_LANG'${NC}"
    echo "Supported languages: en, zh-cn"
    exit 1
fi

# Get the absolute path of this script's directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ ! -f "$SCRIPT_DIR/config.example.json" ]; then
    echo -e "${RED}Please run this script from the SCV project root directory${NC}"
    exit 1
fi

# Source directories
SCV_SKILL_SRC="$SCRIPT_DIR/skills/$SKILL_LANG"
SCV_SCRIPTS_SRC="$SCRIPT_DIR/skills/scripts"
SCV_AGENT_SRC="$SCRIPT_DIR/agents/$SKILL_LANG/project-analyzer.md"

# Target directories
CLAUDE_DIR="$HOME/.claude"
CLAUDE_SKILLS_DIR="$CLAUDE_DIR/skills"
CLAUDE_AGENTS_DIR="$CLAUDE_DIR/agents"
SKILL_TARGET_DIR="$CLAUDE_SKILLS_DIR/scv"
SCV_DATA_DIR="$HOME/.scv"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  SCV (Source Code Vault) Setup${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "${GREEN}OS Detected:${NC} $OS_TYPE"
echo -e "${GREEN}Language:${NC} $SKILL_LANG"
echo -e "${GREEN}Source:${NC}"
echo "  Skill: $SCV_SKILL_SRC"
echo "  Scripts: $SCV_SCRIPTS_SRC"
echo "  Agent: $SCV_AGENT_SRC"
echo -e "${GREEN}Target:${NC}"
echo "  Skill: $SKILL_TARGET_DIR"
echo "  Agent: $CLAUDE_AGENTS_DIR/project-analyzer.md"
echo "  Data: $SCV_DATA_DIR"
echo ""

# Windows-specific warnings
if [ "$OS_TYPE" = "windows" ]; then
    echo -e "${YELLOW}⚠ Windows Detected${NC}"
    echo -e "${YELLOW}Note: On Windows, files will be copied instead of linked.${NC}"
    echo -e "${YELLOW}You'll need to re-run this script after making changes to skills.${NC}"
    echo ""
fi

# Validate source directories
if [ ! -d "$SCV_SKILL_SRC" ]; then
    echo -e "${RED}Error: Skill not found: $SCV_SKILL_SRC${NC}"
    echo "Available languages:"
    ls -d "$SCRIPT_DIR/skills/"*/ 2>/dev/null | xargs -n 1 basename | grep -v scripts
    exit 1
fi

if [ ! -f "$SCV_AGENT_SRC" ]; then
    echo -e "${RED}Error: Agent not found: $SCV_AGENT_SRC${NC}"
    echo "Available languages:"
    ls -d "$SCRIPT_DIR/agents/"*/ 2>/dev/null | xargs -n 1 basename
    exit 1
fi

# Function to create directory if it doesn't exist
create_dir() {
    local dir=$1
    if [ ! -d "$dir" ]; then
        echo -e "${YELLOW}Creating directory:${NC} $dir"
        mkdir -p "$dir"
    else
        echo -e "${GREEN}Directory exists:${NC} $dir"
    fi
}

# Function to create symlink (Unix/Mac) or copy (Windows)
create_symlink() {
    local source=$1
    local target=$2
    local name=$3

    if [ "$OS_TYPE" = "windows" ]; then
        # Windows: always remove and re-copy to ensure latest version
        if [ -e "$target" ]; then
            echo -e "${YELLOW}⟳${NC} $name (updating)"
            rm -rf "$target"
        else
            echo -e "${GREEN}+${NC} $name (copying)"
        fi
        if [ -d "$source" ]; then
            cp -r "$source" "$target"
        else
            cp "$source" "$target"
        fi
        echo -e "${GREEN}  Copied${NC}"
    else
        # Unix/Mac: use symbolic links
        if [ -L "$target" ]; then
            local current_target
            current_target=$(readlink "$target")
            if [ "$current_target" = "$source" ]; then
                echo -e "${GREEN}✓${NC} $name (already linked)"
            else
                echo -e "${YELLOW}⟳${NC} $name (updating link)"
                rm "$target"
                ln -s "$source" "$target"
            fi
        elif [ -e "$target" ]; then
            echo -e "${RED}✗${NC} $name (conflict: $target exists and is not a symlink)"
            echo -e "${YELLOW}  Please manually remove or backup:${NC} $target"
            return 1
        else
            echo -e "${GREEN}+${NC} $name (creating link)"
            ln -s "$source" "$target"
        fi
    fi
}

# Step 1: Create SCV data directory
echo -e "\n${BLUE}Step 1: Creating SCV data directory${NC}"
create_dir "$SCV_DATA_DIR"
create_dir "$SCV_DATA_DIR/repos"
create_dir "$SCV_DATA_DIR/analysis"
create_dir "$SCV_DATA_DIR/sessions"

# Copy config if not exists
if [ -f "$SCV_DATA_DIR/config.json" ]; then
    echo -e "${GREEN}config.json already exists, skipping${NC}"
else
    cp "$SCRIPT_DIR/config.example.json" "$SCV_DATA_DIR/config.json"
    echo -e "${GREEN}+${NC} config.json (copied)"
fi
else
    cp "$SCRIPT_DIR/config.example.json" "$SCV_DATA_DIR/config.json"
    echo -e "${GREEN}+${NC} config.json (copied)"
fi

# Step 2: Create Claude directories
echo -e "\n${BLUE}Step 2: Creating Claude directories${NC}"
create_dir "$CLAUDE_DIR"
create_dir "$CLAUDE_SKILLS_DIR"
create_dir "$CLAUDE_AGENTS_DIR"

# Step 3: Install scv skill
echo -e "\n${BLUE}Step 3: Installing scv skill ($SKILL_LANG)${NC}"

# Remove old skill structure if exists
if [ -d "$SKILL_TARGET_DIR" ] && [ ! -L "$SKILL_TARGET_DIR" ]; then
    echo -e "${YELLOW}⟳ Removing old skill directory...${NC}"
    rm -rf "$SKILL_TARGET_DIR"
fi

# Link/copy skill content file by file (to allow scripts to coexist)
create_dir "$SKILL_TARGET_DIR"

shopt -s nullglob
skill_files=("$SCV_SKILL_SRC"/*)
shopt -u nullglob

for skill_file in "${skill_files[@]}"; do
    file_name=$(basename "$skill_file")
    create_symlink "$skill_file" "$SKILL_TARGET_DIR/$file_name" "scv/$file_name"
done

# Step 4: Install shared scripts
echo -e "\n${BLUE}Step 4: Installing shared scripts${NC}"
if [ -d "$SCV_SCRIPTS_SRC" ]; then
    create_symlink "$SCV_SCRIPTS_SRC" "$SKILL_TARGET_DIR/scripts" "scv/scripts"
else
    echo -e "${YELLOW}No scripts directory found, skipping${NC}"
fi

# Step 5: Install project-analyzer agent
echo -e "\n${BLUE}Step 5: Installing project-analyzer agent ($SKILL_LANG)${NC}"
create_symlink "$SCV_AGENT_SRC" "$CLAUDE_AGENTS_DIR/project-analyzer.md" "project-analyzer.md"

# Step 6: Verification
echo -e "\n${BLUE}Step 6: Verification${NC}"

echo -e "\n${YELLOW}scv skill (~/.claude/skills/scv/):${NC}"
if [ -d "$SKILL_TARGET_DIR" ]; then
    ls -lh "$SKILL_TARGET_DIR" | tail -n +2 | awk '{print "  " $NF}'
else
    echo -e "${RED}  skill directory not found${NC}"
fi

echo -e "\n${YELLOW}project-analyzer agent:${NC}"
if [ -L "$CLAUDE_AGENTS_DIR/project-analyzer.md" ]; then
    echo -e "${GREEN}  ✓ linked${NC}"
    ls -lh "$CLAUDE_AGENTS_DIR/project-analyzer.md" | awk '{print "  " $9 " -> " $11}'
elif [ -f "$CLAUDE_AGENTS_DIR/project-analyzer.md" ]; then
    echo -e "${GREEN}  ✓ installed (Windows copy)${NC}"
else
    echo -e "${RED}  ✗ not installed${NC}"
fi

# Step 7: Summary
echo -e "\n${BLUE}========================================${NC}"
echo -e "${GREEN}SCV installation complete!${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "${GREEN}Configuration:${NC}"
echo "  Language: $SKILL_LANG"
echo "  Data directory: $SCV_DATA_DIR"
echo "  Skill: $SKILL_TARGET_DIR"
echo "  Agent: $CLAUDE_AGENTS_DIR/project-analyzer.md"
echo ""
echo -e "${GREEN}You can now use SCV commands in Claude Code:${NC}"
echo "  /scv run <path|url>  - Analyze a single repository"
echo "  /scv batchRun        - Batch analyze multiple repositories (parallel)"
echo "  /scv gather <opts>   - Clone and manage repositories"
echo ""
if [ "$OS_TYPE" = "windows" ]; then
    echo -e "${YELLOW}Windows Notes:${NC}"
    echo "  - Files are copied, not linked"
    echo "  - Re-run this script after updating skills"
    echo "  - To remove: rm -rf $SCV_DATA_DIR $SKILL_TARGET_DIR $CLAUDE_AGENTS_DIR/project-analyzer.md"
else
    echo -e "${GREEN}Notes:${NC}"
    echo "  - Changes to skills/agents in this project are immediately reflected"
    echo "  - To switch language, run: $0 --lang=<en|zh-cn>"
    echo "  - To remove: rm -rf $SCV_DATA_DIR $SKILL_TARGET_DIR $CLAUDE_AGENTS_DIR/project-analyzer.md"
fi
echo ""
echo -e "${GREEN}For more information, see README.md${NC}"
