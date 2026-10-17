// A separate deployment artifact: no server keys, profiles or messages included.
import {build} from 'esbuild';
import {mkdir,readFile,writeFile,copyFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';
const source=new URL('../core/webapp/assets/',import.meta.url),out=new URL('../dist/pages/',import.meta.url);
await mkdir(out,{recursive:true});
const files=['index.html','style.css','manifest.webmanifest','icon.svg','icon-192.png','icon-512.png','licenses.txt'];
for(const file of files)await copyFile(new URL(file,source),new URL(file,out));
await build({entryPoints:[fileURLToPath(new URL('./src/app.js',import.meta.url))],bundle:true,minify:true,format:'esm',target:['safari16.4'],define:{__BLIZKO_PAGES__:'true'},outfile:fileURLToPath(new URL('app.js',out)),legalComments:'eof'});
const policy="default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; connect-src 'self' https://raw.githubusercontent.com wss:; worker-src 'self'; manifest-src 'self'; media-src 'self' blob:; base-uri 'none'; form-action 'none'; object-src 'none'";
let html=await readFile(new URL('index.html',out),'utf8');
html=html.replace('<head>','<head><meta http-equiv="Content-Security-Policy" content="'+policy+'"><meta name="referrer" content="no-referrer">');
await writeFile(new URL('index.html',out),html);
const hash=createHash('sha256');for(const name of [...files,'app.js'])hash.update(await readFile(new URL(name,out)));
const sw=await readFile(new URL('./src/sw.js',import.meta.url),'utf8');hash.update(sw);const revision=hash.digest('hex').slice(0,16);
await writeFile(new URL('sw.js',out),sw.replace('__BUILD__','pages-'+revision));
await writeFile(new URL('.nojekyll',out),'');
console.log('Built Pages shell '+revision);
