# OneClick Dev Server

> **PROJECT SOURCE OF TRUTH** — Đọc file này trước khi sửa code. Mọi thay đổi kiến trúc, logic, file mới, milestone và quyết định kỹ thuật phải được cập nhật vào README trong cùng commit/PR.

## 1. Mục tiêu dự án

OneClick Dev Server là một ứng dụng cross-platform giúp chọn một website/project đang nằm trên máy local và chạy/public nó trong **môi trường VM cô lập riêng**, thay vì chạy source không tin cậy trực tiếp trên máy host.

Mục tiêu trải nghiệm:

```text
Mở OneClick Dev Server
        ↓
Tự tìm project local
        ↓
Chọn project
        ↓
Create isolated environment
        ↓
1 project = 1 VM riêng
        ↓
Web runtime + DB + disk + tunnel riêng
        ↓
Start / Stop / Sync / Reset / Delete / Public
```

### Windows

Nguồn project mặc định:

```text
C:\xampp\htdocs\*
C:\laragon\www\*
```

Virtualization backend mục tiêu: **Hyper-V**.

### Ubuntu/Linux

Nguồn project mặc định:

```text
/var/www/*
~/www/*
~/projects/*
```

Virtualization backend mục tiêu: **KVM/QEMU/libvirt**.

Có thể thêm root tùy chỉnh bằng biến môi trường `ONECLICK_PROJECT_ROOTS`.

---

## 2. Quy tắc bảo mật bắt buộc

### Core invariant

**1 project = 1 VM.**

Không chạy nhiều site không tin cậy chung một VM nếu mục tiêu isolation vẫn còn hiệu lực.

### Source trên host chỉ là nguồn đọc/copy

Ví dụ:

```text
C:\xampp\htdocs\site-a
```

không bao giờ được dùng làm document root trực tiếp của guest và không được bind-mount/shared-folder vào VM.

Source phải được **copy một chiều** vào VM. Guest không có writable path quay lại source gốc trên host.

### Không malware scan gate

Dự án không dựa vào việc quét malware trước khi chạy. Mọi project được coi là **untrusted từ đầu**. Bảo mật đến từ containment/isolation.

### Không chia sẻ tài nguyên giữa site

Mỗi site phải có riêng:

- VM
- writable disk
- web runtime
- database
- network identity
- Cloudflare Tunnel/public tunnel

Không dùng chung MySQL/XAMPP/Laragon runtime trên host.

### Không đưa dữ liệu host vào guest

Guest không được nhận:

- host credentials
- SSH keys
- browser profile
- Git credentials
- clipboard/drive mapping như một dependency
- shared writable host folder

### Network isolation

VM site phải bị chặn truy cập:

- host management address
- private LAN không cần thiết
- VM của site khác

Guest chỉ cần outbound Internet cho dependency và tunnel. IPv6 phải có rule tương đương hoặc bị disable trên sandbox network để không bypass IPv4 isolation.

### Security wording

VM/hypervisor là security boundary mạnh nhưng **không được mô tả là tuyệt đối 100%** chống mọi hypervisor vulnerability.

Chi tiết threat model cũ hiện vẫn nằm ở `docs/isolation-architecture.md`; khi logic trong README và file đó mâu thuẫn, **README là nguồn chuẩn mới hơn**.

---

## 3. Kiến trúc hiện tại

Dự án đang chuyển từ prototype .NET/WPF sang **Go + embedded web UI** để chạy được Windows và Linux mà người dùng không cần cài .NET SDK.

Kiến trúc mục tiêu:

```text
┌────────────────────────────────────────────┐
│ OneClick Dev Server (Go binary)            │
│                                            │
│  HTTP server: 127.0.0.1 only               │
│  Embedded Web UI                           │
│  REST API                                  │
│                                            │
│  Core                                      │
│  ├── Project Discovery                     │
│  ├── Sandbox Lifecycle                     │
│  ├── Source Sync                           │
│  ├── Tunnel Manager                        │
│  └── State / Logs                          │
│                                            │
│  Virtualization Adapter                    │
│  ├── Windows → Hyper-V                     │
│  └── Linux   → KVM/QEMU/libvirt            │
└────────────────────────────────────────────┘
```

Ứng dụng cuối cùng nên build thành binary độc lập:

```text
Windows: oneclick-dev-server.exe
Linux:   oneclick-dev-server
```

UI chạy local và chỉ bind loopback, ví dụ:

```text
http://127.0.0.1:3765
```

Không bind `0.0.0.0` mặc định cho management UI.

---

## 4. Trạng thái triển khai hiện tại

### Đã có trên `main` trước migration Go

Prototype .NET/WPF đã có:

- Windows desktop scaffold
- Hyper-V/virtualization/admin prerequisite check
- phát hiện project XAMPP/Laragon
- sandbox identity/plan
- thiết kế `one project = one VM`
- CI Windows

Prototype này nằm trong `src/OneClickDevServer/` và hiện là **legacy/reference implementation**, không phải kiến trúc dài hạn.

### Đang triển khai trên `feat/go-cross-platform`

Đã thêm:

- `go.mod` — Go module mới
- `internal/project/discovery.go` — project discovery cross-platform
- Windows roots: XAMPP + Laragon
- Linux roots: `/var/www`, `~/www`, `~/projects`
- custom roots bằng `ONECLICK_PROJECT_ROOTS`
- bỏ qua symlink entry trong automatic discovery

### Chưa triển khai

- Go application entry point
- local HTTP API
- embedded web UI
- browser auto-open
- Hyper-V adapter bằng Go orchestration
- KVM/libvirt adapter
- golden base image manager
- VM creation thật
- differencing disk / qcow2 overlay
- isolated NAT/network ACL
- one-way source transfer thật
- guest bootstrap PHP/Apache/Nginx/MariaDB
- per-site Cloudflare Tunnel
- Start/Stop/Sync/Reset/Delete lifecycle thật
- binary release artifacts cho Windows/Linux

---

## 5. File map — đọc phần này để biết sửa ở đâu

### Cross-platform Go implementation — kiến trúc chính mới

| File / folder | Trách nhiệm | Sửa khi nào |
|---|---|---|
| `go.mod` | Go module/version | thay Go version hoặc dependencies |
| `internal/project/discovery.go` | tìm project local theo OS | thêm XAMPP/Laragon path, Linux roots, custom source roots, filtering |
| `cmd/oneclick-dev-server/main.go` | **planned** entry point | startup, flags, port, shutdown, browser open |
| `internal/server/` | **planned** local HTTP/API server | endpoints, loopback binding, middleware |
| `internal/web/` | **planned** embedded frontend assets | UI dashboard và static embedding |
| `internal/platform/` | **planned** adapter interface | contract chung Hyper-V/KVM |
| `internal/platform/hyperv/` | **planned** Windows backend | Hyper-V VM/disk/network lifecycle |
| `internal/platform/libvirt/` | **planned** Linux backend | KVM/QEMU/libvirt lifecycle |
| `internal/sandbox/` | **planned** sandbox model/state | create/start/stop/reset/delete orchestration |
| `internal/sync/` | **planned** one-way source sync | copy source host → guest, không shared folder |
| `internal/tunnel/` | **planned** public tunnel | Cloudflare Tunnel per VM |
| `internal/state/` | **planned** persistent metadata | lưu sandbox IDs, paths, status, config |

### Documentation

| File | Trách nhiệm |
|---|---|
| `README.md` | **single source of truth của toàn dự án**. Phải update mỗi khi logic/structure thay đổi. |
| `docs/isolation-architecture.md` | tài liệu isolation giai đoạn prototype; giữ để tham khảo cho đến khi migration hoàn tất |

### Legacy .NET/WPF prototype

Folder:

```text
src/OneClickDevServer/
```

Vai trò hiện tại: reference/prototype. Không mở rộng feature mới ở đây trừ khi cần sửa khẩn cấp cho bản legacy.

Các file chính:

| File | Logic cũ |
|---|---|
| `MainWindow.xaml` | WPF UI prototype |
| `MainWindow.xaml.cs` | event flow project discovery/safety/sandbox plan |
| `Services/ProjectDiscoveryService.cs` | XAMPP/Laragon discovery cũ |
| `Services/SandboxPlanner.cs` | sandbox ID/path plan cũ |
| `Services/SafetyCheckService.cs` | prerequisite checks cũ |
| `Services/PowerShellRunner.cs` | PowerShell invocation wrapper cũ |
| `Models/*` | model prototype |
| `OneClickDevServer.csproj` | .NET 10 WPF project |

Khi Go migration đạt feature parity và binary Go chạy ổn, folder legacy này sẽ được xóa trong một PR riêng.

---

## 6. Runtime flow mục tiêu

### Startup

```text
binary start
   ↓
detect OS
   ↓
select virtualization adapter
   ↓
start HTTP server on 127.0.0.1
   ↓
load embedded UI
   ↓
discover local projects
```

### Create environment

```text
User selects project
   ↓
Generate deterministic sandbox ID
   ↓
Prepare isolated VM disk from golden base
   ↓
Create dedicated VM
   ↓
Create isolated network identity/rules
   ↓
One-way copy source into guest
   ↓
Bootstrap web runtime + DB
   ↓
Start site
```

### Public

```text
User clicks Public
   ↓
cloudflared runs INSIDE that site's VM
   ↓
Tunnel → guest web server
   ↓
Return public HTTPS URL to UI
```

Tunnel không chạy trên host nếu nó phục vụ untrusted site.

### Sync

```text
Host source changed
   ↓
User clicks Sync
   ↓
Create clean one-way transfer snapshot/package
   ↓
Guest imports it
   ↓
No writable guest reference to host source
```

Trên Hyper-V, thiết kế dự kiến dùng transfer VHDX transient hoặc cơ chế tương đương đảm bảo one-way semantics. Trên Linux adapter có thể dùng một transient image/archive path với security properties tương đương.

### Reset

Reset phải chỉ ảnh hưởng site được chọn:

```text
site-a reset
  → destroy site-a writable state
  → recreate from clean base

site-b
  → untouched
```

---

## 7. Data layout mục tiêu

Không lưu runtime sandbox vào source tree của user.

Ví dụ Windows:

```text
%ProgramData%\OneClickDevServer\
├── images\
│   └── base.*
├── sandboxes\
│   ├── site-a-<hash>\
│   └── site-b-<hash>\
└── state\
```

Linux:

```text
/var/lib/oneclick-dev-server/
├── images/
├── sandboxes/
└── state/
```

Exact paths có thể thay đổi khi implementation bắt đầu; nếu thay, update README ngay.

---

## 8. Sandbox identity

Sandbox ID phải deterministic từ project name + normalized absolute source path để hai folder trùng tên không collision.

Prototype .NET đã dùng dạng:

```text
<safe-project-name>-<short-path-hash>
```

Go implementation nên giữ nguyên ý tưởng để migration state dễ hiểu.

Không dùng raw user path làm shell command text mà không escaping/structured execution.

---

## 9. API/UI nguyên tắc

Management API/UI:

- bind `127.0.0.1` mặc định
- không public management UI qua Cloudflare Tunnel
- endpoints thay đổi state phải validate project/sandbox ID
- không nhận arbitrary shell command từ browser
- frontend không trực tiếp thực thi host command
- host command execution nằm trong platform adapter với input có cấu trúc

UI mục tiêu:

```text
Projects
├── site-a   XAMPP
├── site-b   Laragon
└── site-c   Projects

Selected site
├── Create Environment
├── Start
├── Stop
├── Sync
├── Reset
├── Delete
└── Public
```

---

## 10. Build và release strategy

### Development

CI phải build/test Go trên ít nhất:

- Windows
- Ubuntu

### Release mục tiêu

Cross-compile hoặc matrix build tạo:

```text
oneclick-dev-server-windows-amd64.exe
oneclick-dev-server-linux-amd64
```

Có thể thêm arm64 sau.

Người dùng cuối **không cần Go SDK** nếu tải release binary.

Trong giai đoạn dev, người clone source để `go run` cần Go SDK. Sau khi GitHub Actions xuất artifact/release binary, việc test thường ngày nên dùng binary đó.

---

## 11. Quy tắc phát triển từ bây giờ

Mỗi feature phải theo flow:

```text
branch nhỏ
   ↓
code
   ↓
update README trong cùng PR
   ↓
CI Windows + Linux pass
   ↓
merge main
   ↓
main luôn có bản test được
```

### README update checklist bắt buộc

Khi thêm/sửa feature, cập nhật ít nhất những phần bị ảnh hưởng:

1. `Trạng thái triển khai hiện tại`
2. `File map`
3. `Runtime flow` nếu logic đổi
4. `Security rules` nếu boundary đổi
5. `Changelog`
6. `Next milestone`

Nếu tạo file mới mà file map chưa có → PR chưa hoàn thành.

Nếu thay đổi behavior mà README vẫn mô tả behavior cũ → PR chưa hoàn thành.

---

## 12. Changelog kỹ thuật

### 2026-08-22 — Go cross-platform migration started

- quyết định bỏ .NET/WPF làm kiến trúc dài hạn
- chọn Go cho core/binary cross-platform
- chọn embedded local web UI
- Windows backend mục tiêu: Hyper-V
- Linux backend mục tiêu: KVM/QEMU/libvirt
- thêm `go.mod`
- thêm `internal/project/discovery.go`
- project discovery Windows: XAMPP + Laragon
- project discovery Linux: `/var/www`, `~/www`, `~/projects`
- thêm `ONECLICK_PROJECT_ROOTS`
- README trở thành project source of truth

### 2026-08-22 — Isolation model established

- bỏ malware scanning gate
- mọi source được coi là untrusted
- thiết lập invariant `1 project = 1 VM`
- source host chỉ read/copy, không mount trực tiếp vào guest
- mỗi site có disk/runtime/DB/tunnel riêng
- VM phải bị isolate khỏi host/LAN/site khác
- tunnel của site chạy trong guest

### 2026-08-22 — Initial Windows prototype

- tạo .NET 10 WPF scaffold
- project discovery XAMPP/Laragon
- safety/prerequisite checks
- sandbox planner
- Windows CI
- prototype được merge vào `main` trước khi quyết định migrate sang Go

---

## 13. Next milestone

Milestone đang làm: **Go runnable dashboard v1**.

Definition of done:

- [ ] `cmd/oneclick-dev-server/main.go`
- [ ] HTTP server bind loopback
- [ ] embedded HTML/CSS/JS UI
- [ ] `/api/system` trả OS/backend target
- [ ] `/api/projects` trả project discovery hiện tại
- [ ] Windows binary build thành công
- [ ] Linux binary build thành công
- [ ] CI matrix Windows + Ubuntu
- [ ] release/build artifact để test mà không cần .NET
- [ ] README cập nhật trạng thái sau khi hoàn tất

Milestone sau đó: **virtualization adapter + create isolated VM thật**.

---

## 14. Cách tiếp cận khi một developer/AI mới vào dự án

Không quét toàn repo trước.

Thứ tự đọc:

```text
1. README.md
2. File được chỉ ra trong File map cho feature cần sửa
3. Chỉ đọc dependency trực tiếp của file đó nếu cần
```

Ví dụ:

- sửa project discovery → đọc `internal/project/discovery.go`
- sửa Hyper-V → đọc `internal/platform/hyperv/` sau khi folder được tạo
- sửa KVM → đọc `internal/platform/libvirt/`
- sửa API → đọc `internal/server/`
- sửa UI → đọc `internal/web/`
- sửa sandbox lifecycle → đọc `internal/sandbox/`
- sửa tunnel → đọc `internal/tunnel/`

Mục tiêu của README này là để một người mới có thể hiểu **project làm gì, security boundary là gì, code nằm ở đâu, hiện đã làm tới đâu và bước tiếp theo là gì** mà không phải index/quét lại toàn bộ repository.
