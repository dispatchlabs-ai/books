// Preserve a pre-session operator-owned feedback page while adding revision checks.
import { readFile, writeFile } from "node:fs/promises";
const [source, target] = process.argv.slice(2);
let html = await readFile(source, "utf8");
for (const [before, after] of [
  [
    "headers:{'Content-Type':'application/json'},body:payload",
    "headers:{'Content-Type':'application/json','If-Match':window.booksAnswerRevision},body:payload",
  ],
  [
    "if(!r.ok)throw Error();if(v===version)",
    "if(r.status===409)throw Error('Notes changed in another tab. Copy your unsaved text, then reload.');if(!r.ok)throw Error();window.booksAnswerRevision=r.headers.get('etag');if(v===version)",
  ],
  [
    "fetch('api/items').then(r=>r.json())",
    "fetch('api/items').then(r=>{window.booksAnswerRevision=r.headers.get('etag');return r.json()})",
  ],
  [
    "status.textContent='Could not save — please keep this page open'",
    "status.textContent=e.message||'Could not save — please keep this page open'",
  ],
]) {
  if (!html.includes(before))
    throw new Error("Unrecognized feedback page; review before migrating");
  html = html.replace(before, after);
}
await writeFile(target, html, { mode: 0o600 });
