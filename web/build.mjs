import {build} from 'esbuild';
import {readFile,writeFile,mkdir,copyFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';
const out=new URL('../core/webapp/assets/',import.meta.url);
await mkdir(out,{recursive:true});
await build({entryPoints:[fileURLToPath(new URL('./src/app.js',import.meta.url))],bundle:true,minify:true,format:'esm',target:['safari16.4'],outfile:fileURLToPath(new URL('app.js',out)),legalComments:'eof'});
for(const name of ['index.html','style.css','manifest.webmanifest','icon.svg','icon-192.png','icon-512.png']) {
 const source=new URL('./src/'+name,import.meta.url),target=new URL(name,out);
 if(name.endsWith('.png'))await copyFile(source,target);
 else await writeFile(target,(await readFile(source,'utf8')).replaceAll('\r\n','\n'));
}
const digest=createHash('sha256');for(const name of ['app.js','style.css','index.html','manifest.webmanifest','icon.svg','icon-192.png','icon-512.png'])digest.update(await readFile(new URL(name,out)));
digest.update((await readFile(new URL('./src/sw.js',import.meta.url),'utf8')).replaceAll('\r\n','\n'));
const version=digest.digest('hex').slice(0,16);
await writeFile(new URL('sw.js',out),(await readFile(new URL('./src/sw.js',import.meta.url),'utf8')).replaceAll('\r\n','\n').replace('__BUILD__',version));
let licenses='Blizko Web — third party licenses\n\n';
for(const [pkg,file] of [['tweetnacl','LICENSE'],['jsqr','LICENSE'],['qrcode','license'],['dijkstrajs','LICENSE.md'],['pngjs','LICENSE']])licenses+='\n'+pkg+'\n'+await readFile(new URL('./node_modules/'+pkg+'/'+file,import.meta.url),'utf8');
await writeFile(new URL('licenses.txt',out),licenses);
console.log('Built web shell '+version);
