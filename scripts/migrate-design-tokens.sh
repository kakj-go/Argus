#!/bin/bash

# Design Token Migration Script
# 将所有旧的 --space-* token 迁移到新的 --space-px-* token

set -e

echo "开始迁移 design tokens..."

# Token 映射关系
# --space-2: 8px  → --space-px-8
# --space-3: 12px → --space-px-12
# --space-4: 16px → --space-px-16
# --space-5: 20px → --space-px-20
# --space-6: 24px → --space-px-24
# --space-8: 32px → --space-px-32
# --space-10: 40px → --space-px-40

TARGET_DIR="web/apps/enterprise/src/styles"

# 备份
echo "创建备份..."
BACKUP_DIR=".design-token-migration-backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"
cp -r "$TARGET_DIR" "$BACKUP_DIR/"
echo "备份完成: $BACKUP_DIR"

# 执行替换
echo "执行 token 替换..."

find "$TARGET_DIR" -name "*.css" -type f | while read -r file; do
  echo "处理: $file"

  # 使用 sed 进行替换
  # 注意：需要排除已经是 --space-px- 开头的情况
  sed -i.bak '
    s/var(--space-10)/var(--space-px-40)/g
    s/var(--space-8)/var(--space-px-32)/g
    s/var(--space-6)/var(--space-px-24)/g
    s/var(--space-5)/var(--space-px-20)/g
    s/var(--space-4)/var(--space-px-16)/g
    s/var(--space-3)/var(--space-px-12)/g
    s/var(--space-2)/var(--space-px-8)/g
  ' "$file"

  # 删除备份文件
  rm -f "$file.bak"
done

echo "Token 替换完成！"
echo ""
echo "请执行以下步骤验证："
echo "1. 运行前端构建: cd web && npm run build"
echo "2. 启动开发服务器: npm run dev"
echo "3. 视觉检查所有界面"
echo "4. 如果有问题，可以从备份恢复: cp -r $BACKUP_DIR/styles/* $TARGET_DIR/"
echo ""
echo "验证通过后，可以删除备份: rm -rf $BACKUP_DIR"
