const {chromium}=require('playwright');
const fs=require('node:fs/promises');
const path=require('node:path');
const {ready,measure}=require('./browser-audit.cjs');
const out=__dirname;
const results=[];
async function override(page,methods) {
  await page.evaluate(async methods=>{
    const {app}=await import('/src/lib/bridge.ts');
    const generated=await import('/wailsjs/go/main/App.js');
    const bound=Object.fromEntries(Object.keys(generated).map(k=>[k,app[k]]));
    window.go={main:{App:new Proxy(bound,{get(target,prop){
      if(!(prop in methods)) return target[prop];
      return async()=>{const spec=methods[prop];if(spec.error)throw new Error(spec.error);return spec.value;};
    }})}};
  },methods);
}
const catalog={supported:true,platform:'windows',hardware:{platform:'windows',supported:true,gpus:[],memoryTotalMiB:16000,recommendedRuntime:'audit-cpu'},runtimes:[],installedModels:[],models:[{id:'audit-model',name:'Synthetic Model',description:'Audit fixture',vision:true,toolUse:true,license:'test'}],downloads:[],status:{state:'stopped',supported:true,installed:false},modelsDirectory:'D:/synthetic-models'};
async function settings(page) {await page.locator('.sidebar__navitem').last().focus();await page.keyboard.press('Enter');await page.locator('.settings-modal').waitFor();}
async function main(){
  const browser=await chromium.launch({headless:true,executablePath:'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'});
  try{
    for(const scenario of ['settings-error','local-null','local-error','install-error','paused-download','cancelled-download','running-controls']){
      const context=await browser.newContext({viewport:{width:1366,height:900},locale:'zh-CN'});
      const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
      await page.addInitScript(()=>{localStorage.setItem('orca-ui-style','modern');localStorage.setItem('orca-lang','zh');});
      await page.goto('http://127.0.0.1:41873/?platform=windows&mock=running');await ready(page);
      if(scenario==='running-controls'){
        await page.locator('.project-tree__topic-main').filter({hasText:'20260523 p3b P&D'}).click();await page.locator('.composer-runstatus').waitFor();
        const states=[];
        await page.locator('.composer-runstatus__pause').click();await page.waitForTimeout(200);states.push({stage:'pause',label:await page.locator('.composer-runstatus__pause').getAttribute('aria-label')});
        await page.locator('.composer-runstatus__pause').click();await page.waitForTimeout(200);states.push({stage:'resume',label:await page.locator('.composer-runstatus__pause').getAttribute('aria-label')});
        await page.keyboard.press('Escape');await page.waitForTimeout(300);states.push({stage:'escape',runningCount:await page.locator('.composer-runstatus').count()});
        results.push({scenario,states,errors});await context.close();continue;
      }
      if(scenario==='settings-error')await override(page,{Settings:{error:'AUDIT_SETTINGS_FAILURE'}});
      if(scenario==='local-null')await override(page,{GetLocalAICatalog:{value:{...catalog,hardware:null,models:null,runtimes:null,installedModels:null,downloads:null,status:null}}});
      if(scenario==='local-error')await override(page,{GetLocalAICatalog:{error:'AUDIT_CATALOG_FAILURE'}});
      if(scenario==='install-error')await override(page,{GetLocalAICatalog:{value:catalog},StartLocalRuntimeInstall:{error:'AUDIT_DISK_FULL'}});
      if(scenario.endsWith('-download'))await override(page,{GetLocalAICatalog:{value:{...catalog,downloads:[{id:'audit-download',targetId:'audit-model',kind:'model',state:scenario.startsWith('paused')?'paused':'cancelled',totalBytes:1000,downloadedBytes:100}]}}});
      await settings(page);
      if(scenario!=='settings-error')await page.locator('.settings-center__navitem').filter({hasText:'本地 AI'}).click();
      if(scenario==='install-error')await page.getByRole('button',{name:'安装 llama.cpp',exact:true}).click();
      await page.waitForTimeout(350);
      const result={scenario,body:await page.locator('.settings-center__content').innerText(),crash:await page.locator('#crash-overlay').count(),errorUI:await page.locator('[role="alert"],.banner--error').allTextContents(),buttons:await page.locator('.settings-center__content button').evaluateAll(es=>es.map(e=>({label:e.getAttribute('aria-label')||e.textContent,disabled:e.disabled}))),errors};
      if(['install-error','paused-download','settings-error'].includes(scenario)){result.screenshot=scenario+'.png';await page.screenshot({path:path.join(out,result.screenshot)});}
      results.push(result);await fs.writeFile(path.join(out,'browser-functional.json'),JSON.stringify(results,null,2));await context.close();
    }
    for(const dpr of [1,1.25,1.5,2]){
      const context=await browser.newContext({viewport:{width:1366,height:900},deviceScaleFactor:dpr,locale:'zh-CN'});const page=await context.newPage();await page.goto('http://127.0.0.1:41873/?platform=windows&mock=fresh');await ready(page);results.push({scenario:'browser-device-scale',...await measure(page)});await context.close();
    }
    await fs.writeFile(path.join(out,'browser-functional.json'),JSON.stringify(results,null,2));console.log(JSON.stringify(results.map(r=>({scenario:r.scenario,crash:r.crash,buttons:r.buttons,states:r.states,errors:r.errors})),null,2));
  }finally{await browser.close();}
}
main().catch(e=>{console.error(e);process.exitCode=1;});
