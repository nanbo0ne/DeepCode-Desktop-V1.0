const { chromium } = require('playwright');
const fs = require('node:fs/promises');
const path = require('node:path');
const out = __dirname;
const base = 'http://127.0.0.1:41873/?platform=windows';

async function ready(page) {
  await page.locator('.startup-splash').waitFor({state:'detached',timeout:12000});
  if (await page.locator('.onboarding__skip').count()) await page.locator('.onboarding__skip').click();
  await page.locator('.layout').waitFor();
}

async function measure(page) {
  return page.evaluate(() => {
    const visible = e => {
      if (!e.checkVisibility({checkOpacity:true,checkVisibilityCSS:true})) return false;
      for (let p=e;p;p=p.parentElement) {
        if (p.tagName === 'DETAILS' && !p.open && !p.querySelector(':scope > summary')?.contains(e)) return false;
      }
      const r=e.getBoundingClientRect(); return r.width>0 && r.height>0;
    };
    const rect = e => {if(!e)return null; const r=e.getBoundingClientRect(); return {x:r.x,y:r.y,w:r.width,h:r.height,right:r.right,bottom:r.bottom};};
    const named = e => ({text:(e.getAttribute('aria-label')||e.textContent||e.getAttribute('title')||'').trim().slice(0,100),class:e.className,rect:rect(e)});
    const groups = ['.app-chrome button,.app-chrome summary,.modern-chrome button,.modern-chrome summary','.composer-card__actions button,.composer-meta button,.composer-modern-footer button'];
    const overlaps=[];
    for(const group of groups) {
      const es=[...document.querySelectorAll(group)].filter(visible);
      for(let i=0;i<es.length;i++) for(let j=i+1;j<es.length;j++) {
        const a=es[i],b=es[j]; if(a.contains(b)||b.contains(a))continue;
        const ra=a.getBoundingClientRect(),rb=b.getBoundingClientRect();
        const dx=Math.min(ra.right,rb.right)-Math.max(ra.left,rb.left),dy=Math.min(ra.bottom,rb.bottom)-Math.max(ra.top,rb.top);
        if(dx>2 && dy>2) overlaps.push({a:named(a),b:named(b),dx,dy});
      }
    }
    const all=[...document.querySelectorAll('button,summary,input,textarea')].filter(visible);
    const clipped=all.filter(e=>{const r=e.getBoundingClientRect();return r.left < -2 || r.right > innerWidth+2;}).map(named);
    const box=s=>rect(document.querySelector(s));
    const main=box('.main'),welcome=box('.welcome'),transcript=box('.transcript'),card=box('.composer-card'),actions=box('.composer-card__actions');
    return {viewport:{w:innerWidth,h:innerHeight,dpr:devicePixelRatio},style:document.documentElement.dataset.uiStyle,layout:document.querySelector('.layout')?.className,overflow:document.documentElement.scrollWidth-innerWidth,overlaps,clipped,main,welcome,transcript,card,actions,welcomeCenterDelta:welcome&&main?(welcome.x+welcome.w/2)-(main.x+main.w/2):null,scrollRightDelta:main&&transcript?main.right-transcript.right:null,actionsRightInset:card&&actions?card.right-actions.right:null,buttons:all.filter(e=>e.closest('.composer-card')).map(named)};
  });
}

async function main() {
  const browser=await chromium.launch({headless:true,executablePath:'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'});
  const reports=[],errors=[],pages=[];
  try {
    for(const style of ['modern','classic']) for(const lang of ['zh','en']) {
      const context=await browser.newContext({viewport:{width:1366,height:900},locale:lang==='zh'?'zh-CN':'en-US'});
      await context.addInitScript(({style,lang})=>{localStorage.setItem('orca-ui-style',style);localStorage.setItem('orca-lang',lang);localStorage.setItem('orca-theme','light');},{style,lang});
      const page=await context.newPage();
      page.on('pageerror',e=>errors.push({style,lang,message:e.message}));
      for(const scenario of ['fresh','running','demo']) {
        await page.setViewportSize({width:1366,height:900});
        await page.goto(`${base}&mock=${scenario}`); await ready(page);
        if(scenario==='running') {
          await page.locator('.project-tree__topic-main').filter({hasText:'20260523 p3b P&D'}).click();
          await page.locator('.composer-runstatus').waitFor();
        }
        const toggle=page.locator(style==='modern'?'.modern-chrome__workspace-actions button[aria-pressed]':'.app-chrome__panel-toggle--right').last();
        if(await toggle.count() && await toggle.getAttribute('aria-pressed')==='true') await toggle.click();
        for(const width of (lang==='zh'?[1920,1366,1024,820,760,580,460,380,360,320]:[1366,760,380])) {
          await page.setViewportSize({width,height:900}); await page.waitForTimeout(120);
          const report={scenario,lang,...await measure(page)}; reports.push(report);
          await fs.writeFile(path.join(out,'browser-results.json'),JSON.stringify({reports,pages,errors},null,2));
          if(lang==='zh' && ((scenario==='running' && [760,360].includes(width)) || (scenario==='fresh' && width===1920))) {
            const file=`${style}-${scenario}-${width}.png`;
            await page.screenshot({path:path.join(out,file)});report.screenshot=file;
          }
        }
      }
      await page.setViewportSize({width:1366,height:900});
      await page.goto(`${base}&mock=demo`); await ready(page);
      try {
        await page.locator('.sidebar__navitem').last().click({timeout:1500});
      } catch(e) {
        pages.push({type:'settings-entry-blocked',style,lang,error:e.message});
        // Keyboard activation separately verifies that the obstruction is visual.
        await page.locator('.sidebar__navitem').last().focus();
        await page.keyboard.press('Enter');
      }
      await page.locator('.settings-modal').waitFor();
      const modal={style,lang,role:await page.locator('.settings-modal').getAttribute('role'),ariaModal:await page.locator('.settings-modal').getAttribute('aria-modal'),escapedFocus:[]};
      await page.locator('.settings-modal button').first().focus();
      for(let i=0;i<55;i++) {
        await page.keyboard.press('Tab');
        const focus=await page.evaluate(()=>({inside:!!document.activeElement.closest('.settings-modal'),tag:document.activeElement.tagName,label:document.activeElement.getAttribute('aria-label')||document.activeElement.textContent?.trim().slice(0,60)}));
        if(!focus.inside)modal.escapedFocus.push({i,...focus});
      }
      pages.push({type:'modal-focus',...modal});
      await fs.writeFile(path.join(out,'browser-results.json'),JSON.stringify({reports,pages,errors},null,2));
      const tabs=await page.locator('.settings-center__navitem').count();
      for(let i=0;i<tabs;i++) {
        await page.locator('.settings-center__navitem').nth(i).click(); await page.waitForTimeout(130);
        pages.push({type:'settings',style,lang,index:i,title:await page.locator('.settings-center__navitem').nth(i).innerText(),content:(await page.locator('.settings-center__content').innerText()).slice(0,1800),...(await measure(page))});
      }
      await context.close();
    }
    await fs.writeFile(path.join(out,'browser-results.json'),JSON.stringify({reports,pages,errors},null,2));
    console.log(JSON.stringify({cases:reports.length,overlaps:reports.filter(x=>x.overlaps.length).map(x=>({style:x.style,lang:x.lang,width:x.viewport.w,scenario:x.scenario,overlaps:x.overlaps})),settingsPages:pages.filter(x=>x.type==='settings').length,focus:pages.filter(x=>x.type==='modal-focus').map(x=>({style:x.style,lang:x.lang,role:x.role,ariaModal:x.ariaModal,escaped:x.escapedFocus.length})),errors},null,2));
  } finally {await browser.close();}
}
module.exports={measure,ready};
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
