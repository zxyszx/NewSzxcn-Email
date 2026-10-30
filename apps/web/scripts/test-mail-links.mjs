// Run from apps/web with PLAYWRIGHT_MODULE and CHROMIUM_EXECUTABLE configured.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import ts from 'typescript'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch(process.env.CHROMIUM_EXECUTABLE ? { executablePath: process.env.CHROMIUM_EXECUTABLE } : {})
try {
  const page = await browser.newPage()
  const source = ts.transpileModule(fs.readFileSync('src/lib/mail-links.ts', 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } }).outputText.replaceAll('export ', '')
  await page.addScriptTag({ content: source })
  await page.setContent('<pre>https://example.com/verify?a=1%5Babc%5D&amp;b=2。</pre><a download href="https://example.com/text"><span>文字链接</span></a><a href="javascript:alert(1)">危险链接</a>')
  await page.evaluate(() => { window.opened = []; bindMailFrameLinks(document, url => window.opened.push(url)) })
  await page.getByText('https://example.com/verify?a=1%5Babc%5D&b=2', { exact: true }).click()
  await page.getByText('文字链接', { exact: true }).click()
  await page.getByText('危险链接', { exact: true }).click()
  assert.deepEqual(await page.evaluate(() => window.opened), ['https://example.com/verify?a=1%5Babc%5D&b=2', 'https://example.com/text'])
  assert.equal(await page.locator('[download]').count(), 0)
  assert.equal(await page.locator('pre a').getAttribute('target'), '_blank')
  assert((await page.locator('pre').innerText()).endsWith('。'))
  console.log('Plain URLs, text links, query strings, download removal and unsafe scheme rejection passed')
} finally { await browser.close() }
