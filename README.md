# OneClick Dev Server

> **PROJECT SOURCE OF TRUTH** — Đọc file này trước khi sửa code. Mọi thay đổi kiến trúc, logic, file mới, milestone và quyết định kỹ thuật phải cập nhật vào README trong cùng commit/PR.

## 1. Mục tiêu

OneClick Dev Server là ứng dụng cross-platform để chọn website/project trên máy local và chạy/public nó trong **VM cô lập riêng**, thay vì chạy source không tin cậy trực tiếp trên host.

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
Runtime + DB + disk + network + tunnel riêng
        ↓
Start / Stop / Sync / Reset / Delete / Public
```

### Project roots mặc định

Windows:

```text
C:\xampp\htdocs\*
C:\laragon\www\*
```

Linux/Ubuntu:

```text
/var/www/*
~/www/*
~/projects/*
```

Root bổ sung: biến môi trường `ONECLICK_PROJECT_ROOTS` theo path-list của hệ điều hành.

### Virtualization backend mục tiêu

```text
Windows → Hyper-V
Linux   → KVM/QEMU/libvirt
```

---

## 2. Security model bắt buộc

### Core invariant: 1 project = 1 VM

Không chạy nhiều site không tin cậy chung một VM. Một site bị compromise không được dùng chung writable disk, DB, runtime hoặc tunnel với site khác.

### Source trên host chỉ để đọc/copy

Ví dụ `C:\xampp\htdocs\site-a` chỉ là source. Nó **không** được dùng làm guest document root và **không** bind-mount/shared-folder vào guest.

Sync phải là one-way: host → guest. Guest không có writable reference quay lại source gốc.

### Không malware scan gate

Mọi source được coi là **untrusted từ đầu**. Dự án dựa vào containment/isolation, không dựa vào việc kết luận source sạch.

### Không chia sẻ tài nguyên giữa site

Mỗi site có riêng:

- VM
- writable disk/overlay
- web runtime
- database
- network identity
- public tunnel

Không dùng MySQL/Apache/PHP của XAMPP/Laragon trên host để chạy site sandbox.

### Không đưa dữ liệu host vào guest

Không đưa vào guest:

- host credentials
- SSH keys
- Git credentials
- browser profile
- host drive mapping
- shared writable folders
- clipboard/Enhanced Session như dependency của runtime

### Network isolation

VM site phải bị chặn chủ động truy cập:

- host management addresses
- private LAN không cần thiết
- VM của site khác

Guest chỉ cần outbound Internet cho dependency và tunnel. IPv6 phải có policy tương đương hoặc bị disable trên sandbox network.

### Public tunnel

Tunnel phục vụ untrusted site phải chạy **bên trong VM của site đó**, không chạy trên host.

### Giới hạn cam kết

Hypervisor là security boundary mạnh nhưng không được mô tả là bảo vệ tuyệt đối 100% khỏi mọi hypervisor vulnerability.

`docs/isolation-architecture.md` là tài liệu prototype cũ; nếu mâu thuẫn, README này là nguồn chuẩn mới hơn.

---

## 3. Kiến trúc chính

Dự án đã chuyển hướng từ .NET/WPF sang **Go + embedded web UI** để build binary độc lập cho Windows/Linux và không buộc user cài .NET.

Go 1.27 là toolchain hiện tại của project.

```text
┌────────────────────────────────────────────┐
│ OneClick Dev Server (Go binary)            │
│                                            │
│  127.0.0.1:3765                            │
│  ├── Embedded Web UI                       │
│  └── REST API                              │
│                                            │
│  Core                                      │
│  ├── Project Discovery                     │
│  ├── Sandbox Lifecycle      (planned)       │
│  ├── Source Sync            (planned)       │
│  ├── Tunnel Manager         (planned)       │
│  └── State / Logs           (planned)       │
│                                            │
│  Platform                                  │
│  ├── Windows → Hyper-V      (planned)       │
│  └── Linux   → libvirt/KVM  (planned)       │
└────────────────────────────────────────────┘
```

Management UI **chỉ bind loopback**. Không đổi sang `0.0.0.0` mặc định và không public management UI qua tunnel của website.

---

## 4. Trạng thái triển khai

### Runnable Go dashboard v1 — đã code trên `feat/go-cross-platform`

Đã có:

- Go module
- application entry point
- HTTP server `127.0.0.1:3765`
- embedded HTML/CSS/JS dashboard
- browser auto-open best-effort
- `GET /api/system`
- `GET /api/projects`
- project discovery Windows: XAMPP + Laragon
- project discovery Linux: `/var/www`, `~/www`, `~/projects`
- custom roots qua `ONECLICK_PROJECT_ROOTS`
- bỏ qua symlink entry trong automatic discovery
- platform target detection: Hyper-V / KVM-libvirt
- test cho custom project root
- CI matrix Windows + Ubuntu
- CI build standalone Windows/Linux binaries
- CI upload binary artifacts

Chưa có:

- Hyper-V adapter thật
- KVM/libvirt adapter thật
- prerequisite detection thật cho hypervisor
- golden base image manager
- VM creation
- VHDX differencing / qcow2 overlay
- isolated NAT/ACL/firewall
- one-way source transfer thật
- guest PHP/Apache/Nginx/MariaDB bootstrap
- Cloudflare Tunnel trong guest
- Start/Stop/Sync/Reset/Delete lifecycle thật
- persistent sandbox state

### Legacy .NET/WPF prototype

`src/OneClickDevServer/` là prototype cũ đã merge vào `main` trước khi quyết định migrate sang Go. Không phát triển feature mới ở đây. Xóa folder này khi Go đạt feature parity tối thiểu.

---

## 5. File map — sửa gì đọc file nào

### Go implementation hiện tại

| File / folder | Trách nhiệm | Sửa khi |
|---|---|---|
| `go.mod` | Go module/toolchain | đổi Go version/dependency |
| `cmd/oneclick-dev-server/main.go` | entry point, server startup, browser open, graceful shutdown | sửa startup/lifecycle process |
| `internal/project/discovery.go` | tìm project local theo OS | thêm roots/filter/discovery behavior |
| `internal/project/discovery_test.go` | test discovery | đổi discovery logic |
| `internal/platform/platform.go` | detect OS và virtualization backend target | đổi platform mapping/status |
| `internal/server/server.go` | local HTTP server và API routes | thêm/sửa API, middleware, bind behavior |
| `internal/web/assets.go` | embed frontend static files | đổi cách đóng gói UI |
| `internal/web/static/index.html` | dashboard UI hiện tại | sửa giao diện/project list/system panel |
| `internal/browser/open.go` | mở browser theo OS | đổi browser launch behavior |
| `.github/workflows/build.yml` | test/build artifact Windows + Ubuntu | đổi CI/build/release pipeline |

### Planned folders

| Folder | Trách nhiệm tương lai |
|---|---|
| `internal/platform/hyperv/` | Hyper-V VM/disk/network lifecycle |
| `internal/platform/libvirt/` | KVM/QEMU/libvirt lifecycle |
| `internal/sandbox/` | create/start/stop/reset/delete orchestration |
| `internal/sync/` | one-way source transfer |
| `internal/tunnel/` | Cloudflare Tunnel per VM |
| `internal/state/` | persistent sandbox metadata/config |

### Documentation

| File | Trách nhiệm |
|---|---|
| `README.md` | **single source of truth** — bắt buộc update khi code/logic/file map đổi |
| `docs/isolation-architecture.md` | isolation notes từ prototype Windows |

### Legacy .NET/WPF

| File/folder | Logic cũ |
|---|---|
| `src/OneClickDevServer/MainWindow.xaml` | WPF UI prototype |
| `src/OneClickDevServer/MainWindow.xaml.cs` | event flow prototype |
| `src/OneClickDevServer/Services/ProjectDiscoveryService.cs` | Windows discovery cũ |
| `src/OneClickDevServer/Services/SandboxPlanner.cs` | sandbox ID/path plan cũ |
| `src/OneClickDevServer/Services/SafetyCheckService.cs` | Hyper-V/admin check cũ |
| `src/OneClickDevServer/Services/PowerShellRunner.cs` | PowerShell wrapper cũ |
| `src/OneClickDevServer/Models/` | models cũ |
| `src/OneClickDevServer/OneClickDevServer.csproj` | .NET 10 WPF project |

---

## 6. Runtime flow hiện tại

```text
oneclick-dev-server binary
        ↓
server.New("127.0.0.1:3765")
        ↓
embedded UI + REST API
        ↓
browser.Open(local URL)
        ↓
/api/system  → platform.Detect()
/api/projects → project.Discover()
```

Nếu browser không tự mở (ví dụ Linux headless), server vẫn chạy và log URL để user mở thủ công.

Hiện dashboard **không có action tạo VM giả**. VirtualizationReady đang false cho đến khi adapter thật được triển khai.

---

## 7. Runtime flow mục tiêu

### Create environment

```text
User selects project
   ↓
Generate deterministic sandbox ID
   ↓
Prepare private overlay from golden base
   ↓
Create dedicated VM
   ↓
Apply network isolation before workload start
   ↓
One-way copy source into guest
   ↓
Bootstrap runtime + DB
   ↓
Start site
```

### Public

```text
User clicks Public
   ↓
cloudflared runs inside selected VM
   ↓
Tunnel → guest web server
   ↓
UI receives HTTPS URL
```

### Sync

```text
Host source changed
   ↓
User clicks Sync
   ↓
Create one-way transfer artifact/disk
   ↓
Guest imports snapshot
   ↓
No guest writable reference to host source
```

Hyper-V design có thể dùng transient transfer VHDX. Linux backend dùng cơ chế tương đương với cùng security property.

### Reset

```text
site-a reset
  → destroy/recreate site-a writable state

site-b
  → untouched
```

---

## 8. Data layout mục tiêu

Không lưu sandbox runtime vào source tree của user.

Windows dự kiến:

```text
%ProgramData%\OneClickDevServer\
├── images\
├── sandboxes\
└── state\
```

Linux dự kiến:

```text
/var/lib/oneclick-dev-server/
├── images/
├── sandboxes/
└── state/
```

Nếu implementation chọn path khác, cập nhật README cùng PR.

---

## 9. Sandbox identity

Sandbox ID phải deterministic từ project name + normalized absolute path để project trùng tên không collision.

Format định hướng:

```text
<safe-project-name>-<short-path-hash>
```

Không đưa raw user path vào shell string bằng nối chuỗi. Platform adapter phải dùng structured arguments/escaped execution.

---

## 10. API/UI rules

Management API/UI:

- chỉ bind loopback mặc định
- không public qua site tunnel
- endpoint thay đổi state phải validate project/sandbox ID
- browser không được gửi arbitrary shell command
- frontend không trực tiếp chạy host commands
- host operations nằm trong platform adapter với input có cấu trúc
- destructive actions sau này phải scoped đúng sandbox ID

API hiện có:

```text
GET /api/system
GET /api/projects
```

UI hiện có:

```text
System panel
Projects list
Refresh projects
```

UI mục tiêu sau VM adapter:

```text
Create Environment
Start
Stop
Sync
Reset
Delete
Public
```

---

## 11. Build và test

CI file: `.github/workflows/build.yml`.

Mỗi PR/main push:

```text
Windows runner ─┐
                ├─ go test ./...
Ubuntu runner  ─┘
                ↓
standalone build
                ↓
artifact upload
```

Artifacts:

```text
oneclick-dev-server-windows-amd64.exe
oneclick-dev-server-linux-amd64
```

Người dùng tải artifact/release binary **không cần cài Go SDK hoặc .NET SDK**.

Người clone source và muốn `go run` thì cần Go SDK.

---

## 12. Quy tắc phát triển

Flow bắt buộc:

```text
branch nhỏ
   ↓
code
   ↓
update README cùng PR
   ↓
CI Windows + Linux pass
   ↓
merge main
   ↓
main luôn có bản test được
```

### README checklist trước merge

Khi feature đổi, kiểm tra:

1. `Trạng thái triển khai`
2. `File map`
3. `Runtime flow`
4. `Security model` nếu boundary đổi
5. `Changelog`
6. `Next milestone`

**File mới không có trong File map → PR chưa hoàn thành.**

**Behavior đổi nhưng README vẫn mô tả behavior cũ → PR chưa hoàn thành.**

---

## 13. Changelog kỹ thuật

### 2026-08-22 — Runnable Go dashboard v1

- thêm `cmd/oneclick-dev-server/main.go`
- local management server bind `127.0.0.1:3765`
- thêm `internal/server/server.go`
- thêm embedded dashboard `internal/web/`
- thêm `/api/system` và `/api/projects`
- thêm browser auto-open best-effort
- thêm platform target detection
- thêm project discovery test
- CI đổi sang Go matrix Windows + Ubuntu
- CI build/upload standalone binaries

### 2026-08-22 — Go cross-platform migration started

- bỏ .NET/WPF làm kiến trúc dài hạn
- chọn Go + embedded web UI
- Windows target: Hyper-V
- Linux target: KVM/QEMU/libvirt
- thêm `go.mod`
- thêm cross-platform project discovery
- README trở thành project source of truth

### 2026-08-22 — Isolation model established

- không malware scan gate
- mọi source coi là untrusted
- `1 project = 1 VM`
- source host chỉ read/copy
- disk/runtime/DB/tunnel riêng từng site
- network isolation host/LAN/site khác
- tunnel chạy trong guest

### 2026-08-22 — Initial Windows prototype

- .NET 10 WPF scaffold
- XAMPP/Laragon discovery
- safety checks
- sandbox planner
- Windows CI
- merge prototype vào `main` trước Go migration

---

## 14. Next milestone

**Virtualization prerequisites + adapter contract.**

Definition of done dự kiến:

- [ ] interface chung cho virtualization backend
- [ ] Windows Hyper-V availability/prerequisite detection
- [ ] Linux KVM/libvirt availability detection
- [ ] `/api/system` trả trạng thái readiness thật
- [ ] UI hiển thị backend readiness và hướng dẫn thiếu prerequisite
- [ ] chưa chạy workload nếu isolation backend chưa ready
- [ ] CI pass Windows + Ubuntu
- [ ] README update

Milestone sau đó: **create isolated VM thật cho một project**.

---

## 15. Hướng dẫn developer/AI mới

Không quét toàn repo trước.

Thứ tự đọc:

```text
1. README.md
2. File trong File map ứng với feature cần sửa
3. Chỉ đọc dependency trực tiếp nếu cần
```

Ví dụ:

- project discovery → `internal/project/discovery.go`
- API → `internal/server/server.go`
- UI → `internal/web/static/index.html`
- startup → `cmd/oneclick-dev-server/main.go`
- browser open → `internal/browser/open.go`
- platform selection → `internal/platform/platform.go`
- Hyper-V sau này → `internal/platform/hyperv/`
- KVM/libvirt sau này → `internal/platform/libvirt/`
- sandbox lifecycle sau này → `internal/sandbox/`
- source sync sau này → `internal/sync/`
- tunnel sau này → `internal/tunnel/`

Mục tiêu của README: một người mới chỉ cần đọc file này để biết **dự án làm gì, boundary bảo mật là gì, code nằm ở đâu, đã làm tới đâu, và bước tiếp theo là gì** mà không cần scan/index lại toàn repository.
