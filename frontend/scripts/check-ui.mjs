import {readFile} from 'node:fs/promises';

const files = ['src/App.css', 'src/style.css'];
const violations = [];

for (const file of files) {
    const source = await readFile(new URL(`../${file}`, import.meta.url), 'utf8');
    for (const match of source.matchAll(/font-size\s*:\s*([0-9.]+)px/gi)) {
        if (Number(match[1]) < 14) {
            const line = source.slice(0, match.index).split('\n').length;
            violations.push(`${file}:${line} dùng font ${match[1]}px`);
        }
    }
}

const appCss = await readFile(new URL('../src/App.css', import.meta.url), 'utf8');
if (!/--font-caption\s*:\s*14px/.test(appCss)) violations.push('App.css: font tối thiểu phải là 14px');
for (const requirement of [':focus-visible', 'prefers-reduced-motion']) {
    if (!appCss.includes(requirement)) violations.push(`App.css thiếu ${requirement}`);
}

const buttonRule = appCss.match(/\.button\s*\{([^}]*)\}/)?.[1] || '';
const tertiaryButtonRule = appCss.match(/\.button\.tertiary\s*\{([^}]*)\}/)?.[1] || '';
const iconButtonRule = appCss.match(/\.notice button,\s*\.icon-button\s*\{([^}]*)\}/)?.[1] || '';
if (!/height\s*:\s*36px/.test(buttonRule)) violations.push('App.css: .button phải cao 36px');
if (/\b(?:min-)?height\s*:/.test(tertiaryButtonRule)) violations.push('App.css: .button.tertiary không được ghi đè chiều cao chuẩn');
if (!/height\s*:\s*36px/.test(iconButtonRule)) violations.push('App.css: nút icon phải cao 36px');
for (const requirement of [
    '.content-stack { width: 100%; max-width: none;',
    '.recovery-tool-page { position: relative; width: 100%; max-width: none;',
]) {
    if (!appCss.includes(requirement)) violations.push(`App.css thiếu full-width contract: ${requirement}`);
}

if (violations.length > 0) {
    console.error(`UI contract không đạt:\n- ${violations.join('\n- ')}`);
    process.exit(1);
}

console.log('UI contract đạt: font >= 14px, danh sách full-width, button cao 36px, có focus-visible và reduced-motion.');
