import fs from "node:fs"
import path from "node:path"

const root = process.cwd()
const srcDir = path.join(root, "src")

const ignoredSegments = [
  `${path.sep}components${path.sep}ui${path.sep}`,
  `${path.sep}dist${path.sep}`,
]

// Business code must use shadcn/ui wrappers for visible UI primitives.
// Semantic/layout tags such as div, span, form, header, main, section and a are allowed.
const forbidden = [
  { tag: "button", replacement: "@/components/ui/button Button" },
  { tag: "input", replacement: "@/components/ui/input Input" },
  { tag: "textarea", replacement: "@/components/ui/textarea Textarea" },
  { tag: "select", replacement: "@/components/ui/select Select" },
  { tag: "table", replacement: "@/components/ui/table Table" },
  { tag: "thead", replacement: "@/components/ui/table TableHeader" },
  { tag: "tbody", replacement: "@/components/ui/table TableBody" },
  { tag: "tr", replacement: "@/components/ui/table TableRow" },
  { tag: "th", replacement: "@/components/ui/table TableHead" },
  { tag: "td", replacement: "@/components/ui/table TableCell" },
  { tag: "dialog", replacement: "@/components/ui/dialog Dialog" },
  { tag: "aside", replacement: "@/components/ui/sidebar Sidebar" },
]

// Product UI rule: do not add small explanatory subtitle text under titles.
// Keep screens clean: use titles, labels, badges and actionable controls only.
const forbiddenComponents = [
  { name: "CardDescription", reason: "不要在卡片标题下添加说明性小字" },
  { name: "DialogDescription", reason: "不要在弹窗标题下添加说明性小字" },
  { name: "SheetDescription", reason: "不要在抽屉标题下添加说明性小字" },
]

const forbiddenClassPatterns = [
  { pattern: /\bbg-gradient-[^\s"`'}]+/, reason: "不要使用渐变背景，保持 shadcn neutral 极简风格" },
  { pattern: /\b(?:from|via|to)-blue-[^\s"`'}]+/, reason: "不要使用蓝色渐变色阶，保持 neutral/black 视觉" },
  { pattern: /\b(?:bg|text|border|ring)-blue-[^\s"`'}]+/, reason: "不要使用蓝色品牌色，保持 neutral/black 视觉" },
  { pattern: /\bshadow-(?:lg|xl|2xl|primary[^\s"`'}]*)/, reason: "不要使用重阴影或营销感阴影，保持 shadcn 默认克制风格" },
]

function walk(dir) {
  const out = []
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) out.push(...walk(full))
    else if (/\.(tsx|jsx)$/.test(entry.name)) out.push(full)
  }
  return out
}

const violations = []
for (const file of walk(srcDir)) {
  if (ignoredSegments.some((segment) => file.includes(segment))) continue
  const rel = path.relative(root, file)
  const content = fs.readFileSync(file, "utf8")
  const lines = content.split(/\r?\n/)
  lines.forEach((line, index) => {
    for (const rule of forbidden) {
      const re = new RegExp(`<${rule.tag}(\\s|>|/)`)
      if (re.test(line)) {
        violations.push({ file: rel, line: index + 1, tag: rule.tag, replacement: rule.replacement, code: line.trim() })
      }
    }
    for (const rule of forbiddenComponents) {
      const re = new RegExp(`\\b${rule.name}\\b`)
      if (re.test(line)) {
        violations.push({ file: rel, line: index + 1, tag: rule.name, replacement: rule.reason, code: line.trim() })
      }
    }
    for (const rule of forbiddenClassPatterns) {
      if (rule.pattern.test(line)) {
        violations.push({ file: rel, line: index + 1, tag: "visual-style", replacement: rule.reason, code: line.trim() })
      }
    }
  })
}

if (violations.length > 0) {
  console.error("\nshadcn/ui rule failed: business TSX must not use native UI primitives.\n")
  for (const v of violations) {
    console.error(`${v.file}:${v.line} <${v.tag}> -> use ${v.replacement}`)
    console.error(`  ${v.code}`)
  }
  console.error("\nAllowed: native semantic/layout tags like div, span, form, header, main, section, a. Official shadcn components under src/components/ui are exempt.\n")
  process.exit(1)
}

console.log("shadcn/ui rule passed: no native UI primitives found in business TSX.")
