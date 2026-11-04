const fs = require('fs');
const cp = require('child_process');

try {
  // Use tar to extract word/document.xml from docx (docx is zip)
  cp.execSync('tar -xf "d:/Downloads/Dropshipping-Technical-Proposal-FA.docx" word/document.xml', { cwd: 'd:/drup' });
  const xml = fs.readFileSync('d:/drup/word/document.xml', 'utf8');
  const clean = xml
    .replace(/<w:p[^>]*>/g, '\n')
    .replace(/<[^>]+>/g, '')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&')
    .replace(/&quot;/g, '"');
  fs.writeFileSync('d:/drup/proposal.txt', clean, 'utf8');
  console.log('SUCCESS! File length:', clean.length);
} catch (e) {
  console.error('Error:', e.message);
}
