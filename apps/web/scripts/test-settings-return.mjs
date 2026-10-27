// Run against a built demo preview. PLAYWRIGHT_MODULE and CHROMIUM_EXECUTABLE
// can point to an existing local browser installation; no account is needed.
import assert from "node:assert/strict"
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
const browser = await chromium.launch(process.env.CHROMIUM_EXECUTABLE ? { executablePath: process.env.CHROMIUM_EXECUTABLE } : {})
try {
  for (const theme of ["light", "dark"]) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
    const errors = []
    page.on("pageerror", error => errors.push(error.message))
    await page.addInitScript(theme => {
      sessionStorage.setItem("newszxcn:demo-auth", "1")
      localStorage.setItem("lanqin:theme", theme)
    }, theme)
    await page.goto(`${process.env.BASE_URL || "http://127.0.0.1:5202/"}#/mail`)
    await page.getByRole("button", { name: "设置", exact: true }).waitFor()
    await page.evaluate(() => {
      window.returnFrames = []
      window.recordReturn = true
      const sample = () => {
        if (!window.recordReturn) return
        window.returnFrames.push({
          workspace: document.documentElement.classList.contains("workspace-ui"),
          dark: document.documentElement.classList.contains("dark"),
          content: !!document.querySelector(".mail-wallpaper, .profile-workspace-theme"),
        })
        requestAnimationFrame(sample)
      }
      requestAnimationFrame(sample)
    })
    for (let i = 0; i < 4; i++) {
      await page.getByRole("button", { name: "设置", exact: true }).click()
      await page.getByRole("button", { name: "返回邮箱", exact: true }).click()
      await page.getByRole("button", { name: "设置", exact: true }).waitFor()
    }
    const frames = await page.evaluate(() => { window.recordReturn = false; return window.returnFrames })
    assert(frames.length > 0)
    assert.equal(frames.filter(f => !f.workspace || !f.content || f.dark !== (theme === "dark")).length, 0, "Navigation exposed a blank canvas or reset the theme")
    assert.deepEqual(errors, [])
    console.log(`${theme}: ${frames.length} frames, 4 round trips, no blank frame or theme reset`)
    await page.close()
  }
} finally {
  await browser.close()
}
