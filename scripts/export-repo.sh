#!/usr/bin/env bash

# 导出代码仓库为文本文件
# 用法: ./scripts/export-repo.sh [输出文件名]

set -euo pipefail

# 默认输出文件名
OUTPUT_FILE="${1:-repo-export.txt}"

# 项目根目录
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$REPO_ROOT"

echo "正在导出代码仓库到 $OUTPUT_FILE ..."

# 清空或创建输出文件
cat > "$OUTPUT_FILE" << 'EOF'
================================================================================
代码仓库导出
================================================================================
EOF

echo "" >> "$OUTPUT_FILE"
echo "导出时间: $(date)" >> "$OUTPUT_FILE"
echo "项目路径: $REPO_ROOT" >> "$OUTPUT_FILE"
echo "" >> "$OUTPUT_FILE"
echo "================================================================================" >> "$OUTPUT_FILE"
echo "" >> "$OUTPUT_FILE"

# 要包含的文件类型
FILE_EXTENSIONS=(
    "*.go"
    "*.proto"
    "*.yml"
    "*.yaml"
    "Makefile"
)

# 要排除的目录和文件
EXCLUDE_PATTERNS=(
    "*/tmp/*"
    "*/vendor/*"
    "*/.git/*"
    "*/node_modules/*"
    "*/.idea/*"
    "*/.vscode/*"
    "*.gen.go"
    "*_pb.go"
    "*_grpc.pb.go"
    "go.mod"
    "go.sum"
    "*.toml"
    "repo-export.txt"
    "*.log"
    "*.sh"
)

# 构建 find 命令的排除参数
EXCLUDE_ARGS=""
for pattern in "${EXCLUDE_PATTERNS[@]}"; do
    EXCLUDE_ARGS="$EXCLUDE_ARGS -path $pattern -prune -o"
done

# 构建 find 命令的文件类型参数
FIND_ARGS=""
for ext in "${FILE_EXTENSIONS[@]}"; do
    if [ -z "$FIND_ARGS" ]; then
        FIND_ARGS="-name $ext"
    else
        FIND_ARGS="$FIND_ARGS -o -name $ext"
    fi
done

# 查找并处理文件
find . $EXCLUDE_ARGS \( $FIND_ARGS \) -type f -print | sort | while read -r file; do
    # 移除开头的 ./
    clean_path="${file#./}"
    
    echo "" >> "$OUTPUT_FILE"
    echo "================================================================================" >> "$OUTPUT_FILE"
    echo "文件: $clean_path" >> "$OUTPUT_FILE"
    echo "================================================================================" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"
    
    cat "$file" >> "$OUTPUT_FILE"
    
    echo "" >> "$OUTPUT_FILE"
done

echo "" >> "$OUTPUT_FILE"
echo "================================================================================" >> "$OUTPUT_FILE"
echo "导出完成" >> "$OUTPUT_FILE"
echo "================================================================================" >> "$OUTPUT_FILE"

echo "✅ 导出完成！"
echo "   输出文件: $OUTPUT_FILE"
echo "   文件大小: $(du -h "$OUTPUT_FILE" | cut -f1)"
