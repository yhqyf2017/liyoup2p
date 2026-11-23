// 升级分发：多通道版本查询（免鉴权）+ 安装包发布/列表（superadmin）。
// version.json 多通道格式：{"pc":{...},"android":{...},"win7":{...},"mac":{...}}；
// 兼容旧单通道扁平格式（读到 pc 通道）；安装包本体由 /update/ 静态托管。
// win7 通道面向 Win7 老系统客户端（gui-walk），安装包为 Inno Setup 双架构安装器；
// mac 通道面向 macOS 客户端（gui-wails darwin 构建），安装包为含 LiLink.app 的 zip。
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// updateInfo 单通道版本信息（与旧单通道格式字段一致）。
type updateInfo struct {
	Version    string `json:"version"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Notes      string `json:"notes"`
	ReleasedAt string `json:"releasedAt"`
}

// channels 多通道版本清单。
type channels struct {
	PC      *updateInfo `json:"pc"`
	Android *updateInfo `json:"android"`
	Win7    *updateInfo `json:"win7"`
	Mac     *updateInfo `json:"mac"`
}

// UpdateAPI 升级 API：Dir 为平台 update 目录（version.json 与安装包）。
type UpdateAPI struct {
	Dir string
}

// readChannels 读取 version.json（多通道优先，兼容旧扁平格式视作 pc 通道）。
func (u *UpdateAPI) readChannels() channels {
	var ch channels
	data, err := os.ReadFile(filepath.Join(u.Dir, "version.json"))
	if err == nil {
		_ = json.Unmarshal(data, &ch)
		if ch.PC == nil {
			var flat updateInfo
			if json.Unmarshal(data, &flat) == nil && flat.Version != "" {
				ch.PC = &flat
			}
		}
	}
	return ch
}

// saveChannels 原子写回 version.json（临时文件 + 同目录 rename）。
func (u *UpdateAPI) saveChannels(ch channels) error {
	data, err := json.MarshalIndent(&ch, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(u.Dir, "version.json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// Latest GET /api/update/latest（免鉴权）：默认/pc 返回 pc 通道扁平字段（兼容旧客户端），
// ch=android / ch=win7 / ch=mac 返回对应通道；通道缺失或文件不存在时 version 为空串。
func (u *UpdateAPI) Latest(w http.ResponseWriter, r *http.Request) {
	ch := u.readChannels()
	var sel *updateInfo
	switch r.URL.Query().Get("ch") {
	case "android":
		sel = ch.Android
	case "win7":
		sel = ch.Win7
	case "mac":
		sel = ch.Mac
	case "", "pc":
		sel = ch.PC
	default:
		Fail(w, http.StatusBadRequest, 1002, "未知更新通道，仅支持 pc / android / win7 / mac")
		return
	}
	if sel == nil {
		sel = &updateInfo{}
	}
	OK(w, *sel)
}

// List GET /api/platform/updates（superadmin）：各通道当前版本。
func (u *UpdateAPI) List(w http.ResponseWriter, r *http.Request) {
	ch := u.readChannels()
	pc := ch.PC
	if pc == nil {
		pc = &updateInfo{}
	}
	an := ch.Android
	if an == nil {
		an = &updateInfo{}
	}
	w7 := ch.Win7
	if w7 == nil {
		w7 = &updateInfo{}
	}
	mac := ch.Mac
	if mac == nil {
		mac = &updateInfo{}
	}
	OK(w, channels{PC: pc, Android: an, Win7: w7, Mac: mac})
}

// Publish POST /api/platform/updates（superadmin，multipart：channel+version+notes+file）：
// 校验扩展名（.zip/.exe/.apk）→ 存 update 目录（LiLink-{channel}-{version}.{ext}）→
// 实算 sha256 → 原子更新 version.json 对应通道。
func (u *UpdateAPI) Publish(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		Fail(w, http.StatusBadRequest, 1001, "multipart 表单解析失败: "+err.Error())
		return
	}
	channel := strings.TrimSpace(r.FormValue("channel"))
	version := strings.TrimSpace(r.FormValue("version"))
	notes := strings.TrimSpace(r.FormValue("notes"))
	if channel != "pc" && channel != "android" && channel != "win7" && channel != "mac" {
		Fail(w, http.StatusBadRequest, 1002, "channel 仅支持 pc / android / win7 / mac")
		return
	}
	if version == "" || strings.ContainsAny(version, `\/:*?"<>| `) || strings.Contains(version, "..") {
		Fail(w, http.StatusBadRequest, 1003, "version 非法（不能为空或含路径字符）")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		Fail(w, http.StatusBadRequest, 1004, "缺少 file 文件字段")
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	if ext != ".zip" && ext != ".exe" && ext != ".apk" {
		Fail(w, http.StatusBadRequest, 1005, "仅支持 .zip / .exe / .apk 安装包")
		return
	}

	if err := os.MkdirAll(u.Dir, 0o755); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "创建更新目录失败: "+err.Error())
		return
	}
	// 先落临时文件并实算 sha256，完成后原子改名（防止半包被下载）
	tmp, err := os.CreateTemp(u.Dir, "upload-*"+ext)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "写入临时文件失败: "+err.Error())
		return
	}
	tmpName := tmp.Name()
	hasher := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, hasher), file)
	tmp.Close()
	if err != nil {
		_ = os.Remove(tmpName)
		Fail(w, http.StatusInternalServerError, 5000, "保存上传内容失败: "+err.Error())
		return
	}
	name := fmt.Sprintf("LiLink-%s-%s%s", channel, version, ext)
	final := filepath.Join(u.Dir, name)
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		Fail(w, http.StatusInternalServerError, 5000, "落盘安装包失败: "+err.Error())
		return
	}

	info := updateInfo{
		Version:    version,
		URL:        "/update/" + name,
		SHA256:     hex.EncodeToString(hasher.Sum(nil)),
		Notes:      notes,
		ReleasedAt: time.Now().Format(time.RFC3339),
	}
	ch := u.readChannels()
	switch channel {
	case "pc":
		ch.PC = &info
	case "android":
		ch.Android = &info
	case "win7":
		ch.Win7 = &info
	case "mac":
		ch.Mac = &info
	}
	if err := u.saveChannels(ch); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "更新 version.json 失败: "+err.Error())
		return
	}
	OK(w, info)
}
