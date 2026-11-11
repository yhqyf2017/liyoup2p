// 生成《LiLink客户端使用说明.docx》：node gen-help-doc.js <输出路径>
// 需要 docx 包：npm install docx
const fs = require("fs");
const {
  Document, Packer, Paragraph, TextRun, Table, TableRow, TableCell,
  AlignmentType, LevelFormat, HeadingLevel, BorderStyle, WidthType, ShadingType,
} = require("docx");

const cjk = { ascii: "Arial", hAnsi: "Arial", eastAsia: "Microsoft YaHei" };
const mono = { ascii: "Consolas", hAnsi: "Consolas", eastAsia: "Microsoft YaHei" };

const border = { style: BorderStyle.SINGLE, size: 1, color: "CCCCCC" };
const borders = { top: border, bottom: border, left: border, right: border };

function h1(text) {
  return new Paragraph({ heading: HeadingLevel.HEADING_1, children: [new TextRun({ text, font: cjk })] });
}
function h2(text) {
  return new Paragraph({ heading: HeadingLevel.HEADING_2, children: [new TextRun({ text, font: cjk })] });
}
function p(text, opts = {}) {
  return new Paragraph({ spacing: { after: 120 }, children: [new TextRun({ text, font: cjk, ...opts })] });
}
function bold(text) {
  return new TextRun({ text, font: cjk, bold: true });
}
function plain(text) {
  return new TextRun({ text, font: cjk });
}
function code(text) {
  return new TextRun({ text, font: mono, size: 22, shading: { fill: "F2F2F2", type: ShadingType.CLEAR } });
}
function bullet(children) {
  return new Paragraph({ numbering: { reference: "bullets", level: 0 }, spacing: { after: 80 }, children });
}
function numbered(children) {
  return new Paragraph({ numbering: { reference: "numbers", level: 0 }, spacing: { after: 80 }, children });
}
// 图片占位：居中灰字空框，用户自行粘贴截图后删除此行
function imgPlaceholder(label) {
  return new Paragraph({
    alignment: AlignmentType.CENTER,
    spacing: { before: 120, after: 240 },
    border: { top: border, bottom: border, left: border, right: border },
    children: [new TextRun({ text: `【${label} —— 请在此处粘贴截图，粘贴后删除本提示框】`, font: cjk, color: "999999", size: 22 })],
  });
}
function cell(children, w, fill) {
  return new TableCell({
    borders,
    width: { size: w, type: WidthType.DXA },
    shading: fill ? { fill, type: ShadingType.CLEAR } : undefined,
    margins: { top: 80, bottom: 80, left: 120, right: 120 },
    children: [new Paragraph({ children })],
  });
}

const BASE = "https://jyly8888.gnway.org:880";

const doc = new Document({
  styles: {
    default: {
      document: { run: { font: cjk, size: 22 } },
    },
    paragraphStyles: [
      { id: "Heading1", name: "Heading 1", basedOn: "Normal", next: "Normal", quickFormat: true,
        run: { size: 30, bold: true, font: cjk, color: "1F4E79" },
        paragraph: { spacing: { before: 280, after: 160 }, outlineLevel: 0 } },
      { id: "Heading2", name: "Heading 2", basedOn: "Normal", next: "Normal", quickFormat: true,
        run: { size: 25, bold: true, font: cjk, color: "333333" },
        paragraph: { spacing: { before: 200, after: 120 }, outlineLevel: 1 } },
    ],
  },
  numbering: {
    config: [
      { reference: "bullets", levels: [{ level: 0, format: LevelFormat.BULLET, text: "•", alignment: AlignmentType.LEFT,
        style: { paragraph: { indent: { left: 620, hanging: 300 } } } }] },
      { reference: "numbers", levels: [{ level: 0, format: LevelFormat.DECIMAL, text: "%1.", alignment: AlignmentType.LEFT,
        style: { paragraph: { indent: { left: 620, hanging: 300 } } } }] },
    ],
  },
  sections: [{
    properties: {
      page: { size: { width: 11906, height: 16838 }, margin: { top: 1200, right: 1440, bottom: 1200, left: 1440 } },
    },
    children: [
      new Paragraph({ alignment: AlignmentType.CENTER, spacing: { before: 200, after: 60 },
        children: [new TextRun({ text: "励联 LiLink 客户端使用说明", font: cjk, size: 40, bold: true, color: "1F4E79" })] }),
      new Paragraph({ alignment: AlignmentType.CENTER, spacing: { after: 300 },
        children: [new TextRun({ text: "适用于 Windows 电脑与 Android 手机", font: cjk, size: 22, color: "666666" })] }),

      h1("一、LiLink 是什么"),
      p("LiLink 用于从外网安全访问公司内网服务（如管理系统等）。安装登录后，软件会在您的电脑/手机上开通一个本地访问入口，用浏览器打开即可，无需配置任何网络。"),

      h1("二、下载安装"),
      bullet([plain("Windows 电脑：下载安装包 "), code(BASE + "/update/LiLink-pc-1.1.2.exe"), plain("，双击安装（如遇系统提示，选择「仍要运行/允许」）。")]),
      bullet([plain("Android 手机：下载安装包 "), code(BASE + "/update/LiLink-android-1.1.4.apk"), plain("，打开安装（如提示「未知来源」，选择允许）。")]),

      h1("三、登录账号"),
      p("安装完成后打开软件，按下表填写登录信息："),
      new Table({
        width: { size: 100, type: WidthType.PERCENTAGE },
        columnWidths: [2200, 6826],
        rows: [
          ["服务器地址", [plain("已内置，无需填写")]],
          ["租户", [plain("保持默认（如页面有该项）")]],
          ["用户名", [bold("本人姓名的汉语拼音（如：张三 → zhangsan）")]],
          ["密码", [code("cEI<g0yn]cNQ;mhW")]],
          ["设备名", [plain("保持默认即可")]],
        ].map(([k, v]) => new TableRow({
          cantSplit: true,
          children: [cell([bold(k)], 2200, "D5E8F0"), cell(v, 6826)],
        })),
      }),
      imgPlaceholder("图 1：登录界面"),

      h1("四、使用方法"),
      h2("电脑端"),
      numbered([plain("登录后软件自动连接公司网络，无需其他操作；")]),
      numbered([plain("按管理员通知的入口地址访问，默认为浏览器打开 "), code("http://127.0.0.1:8080"), plain("。")]),
      numbered([plain("窗口最小化或关闭后，软件自动缩到右下角托盘继续运行；开机后也会自动启动并恢复连接。需要彻底退出时，右键托盘图标选择「退出」。")]),
      imgPlaceholder("图 2：连接成功的主界面"),
      h2("手机端"),
      numbered([plain("登录后点击「启动连接」；")]),
      numbered([plain("连接成功后，用手机浏览器打开管理员通知的入口地址即可（默认 "), code("http://127.0.0.1:18081"), plain("）。")]),
      imgPlaceholder("图 3：手机端连接成功界面"),

      h1("五、常见问题"),
      bullet([bold("提示「账号已在其他设备登录」："), plain("同一账号同一时间只能在一台设备使用，请退出另一台设备后再登录。")]),
      bullet([bold("右上角显示「中继」或速度偏慢："), plain("属于当前网络环境下的正常现象，软件会自动尝试优化为直连，不影响使用。")]),
      bullet([bold("页面打不开："), plain("先确认软件正在运行（托盘有图标且显示已连接）；仍不行时重启软件重新登录。")]),
      bullet([bold("软件升级："), plain("电脑端打开软件会自动检查更新；手机端在软件内点击升级即可。安卓第一次升级如自动打开了浏览器，属正常现象，下载完成后点击下载的文件完成安装，之后升级恢复正常。")]),
      bullet([bold("忘记密码 / 无法登录："), plain("请联系管理员重置。")]),

      p("", {}),
      new Paragraph({ spacing: { before: 200 },
        children: [new TextRun({ text: "注意：本密码为公司内部统一使用，请勿转发给外部人员。", font: cjk, bold: true, color: "C00000" })] }),
    ],
  }],
});

Packer.toBuffer(doc).then((buffer) => {
  fs.writeFileSync(process.argv[2], buffer);
  console.log("OK " + process.argv[2]);
});
