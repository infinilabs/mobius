const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

const htmlPath = process.argv[2];
if (!htmlPath) {
  console.error(JSON.stringify({ passed: false, errors: ["Missing target HTML path argument"] }));
  process.exit(1);
}

(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 375, height: 812 } });
  const page = await context.newPage();
  const report = { passed: true, errors: [] };

  // 1. File Size Check
  try {
    const stats = fs.statSync(htmlPath);
    const sizeMB = stats.size / (1024 * 1024);
    if (sizeMB > 5.0) {
      report.passed = false;
      report.errors.push(`File size ${sizeMB.toFixed(2)}MB exceeds 5MB limit`);
    }
  } catch (e) {
    report.passed = false;
    report.errors.push(`Failed to stat file: ${e.message}`);
    console.log(JSON.stringify(report));
    process.exit(1);
  }

  // 2. Network Isolation Check
  const externalRequests = [];
  page.on('request', request => {
    const url = request.url();
    if (!url.startsWith('file:') && !url.startsWith('data:')) {
      externalRequests.push(url);
    }
  });

  // Inject Mock MRAID
  await page.addInitScript(() => {
    window.__mraid_open_called_with = null;
    window.__mraid_state = 'loading';
    window.mraid = {
      getState: () => window.__mraid_state,
      open: (url) => { window.__mraid_open_called_with = url; },
      addEventListener: (event, cb) => {
        if (event === 'ready') {
          setTimeout(() => {
            window.__mraid_state = 'default';
            cb();
          }, 100);
        }
      }
    };
  });

  try {
    await page.goto(`file://${path.resolve(htmlPath)}`, { waitUntil: 'load', timeout: 10000 });
  } catch (e) {
    report.passed = false;
    report.errors.push(`Page load failed: ${e.message}`);
    console.log(JSON.stringify(report));
    await browser.close();
    process.exit(1);
  }

  if (externalRequests.length > 0) {
    report.passed = false;
    report.errors.push(`External network requests detected: ${externalRequests.join(', ')}`);
  }

  // 3. Audio Autoplay Verification
  const isAudioSuspended = await page.evaluate(() => {
    const audios = Array.from(document.querySelectorAll('audio, video'));
    const playing = audios.some(a => !a.paused && a.volume > 0 && !a.muted);
    return !playing;
  });
  if (!isAudioSuspended) {
    report.passed = false;
    report.errors.push("Audio autoplay detected (played before user interaction)");
  }

  // 4. Timer Idle Verification
  let initialTimeText = null;
  const timerSelector = '.timer, #timer, .stat-time';
  const timerExists = await page.locator(timerSelector).count() > 0;
  if (timerExists) {
    initialTimeText = await page.locator(timerSelector).innerText();
    await page.waitForTimeout(2000);
    const postTimeText = await page.locator(timerSelector).innerText();
    if (initialTimeText !== postTimeText) {
      report.passed = false;
      report.errors.push("Timer active before user interaction");
    }
  }

  // 5. Simulate User Interaction
  await page.mouse.click(187, 406);
  await page.waitForTimeout(1000);

  // Check if timer started
  if (timerExists && initialTimeText) {
    const activeTimeText = await page.locator(timerSelector).innerText();
    if (initialTimeText === activeTimeText) {
      report.errors.push("Warning: Timer did not decrement after interaction");
    }
  }

  // 6. Audio Suspension on Visibility Hidden
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', writable: true });
    Object.defineProperty(document, 'hidden', { value: true, writable: true });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.waitForTimeout(500);

  const isAudioMutedOnHide = await page.evaluate(() => {
    const audios = Array.from(document.querySelectorAll('audio, video'));
    const playing = audios.some(a => !a.paused && a.volume > 0 && !a.muted);
    return !playing;
  });
  if (!isAudioMutedOnHide) {
    report.passed = false;
    report.errors.push("Audio active when page visibility hidden");
  }

  // Restore visibility
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { value: 'visible', writable: true });
    Object.defineProperty(document, 'hidden', { value: false, writable: true });
    document.dispatchEvent(new Event('visibilitychange'));
  });

  // 7. No Auto-Click Verification
  const mraidOpenCalled = await page.evaluate(() => window.__mraid_open_called_with);
  if (mraidOpenCalled) {
    report.passed = false;
    report.errors.push(`Auto-click detected: MRAID open triggered without CTA tap: ${mraidOpenCalled}`);
  }

  // Click CTA
  const ctaSelector = '.cta-btn, .cta, button, #cta-button';
  const ctaExists = await page.locator(ctaSelector).count() > 0;
  if (ctaExists) {
    await page.click(ctaSelector);
    const redirectUrl = await page.evaluate(() => window.__mraid_open_called_with);
    if (!redirectUrl) {
      report.passed = false;
      report.errors.push("CTA click failed to trigger mraid.open(url)");
    }
  }

  // 8. Landscape Resize Check
  await page.setViewportSize({ width: 812, height: 375 });
  await page.waitForTimeout(500);

  console.log(JSON.stringify(report));
  await browser.close();
})();
