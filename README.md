# OneClick Dev Server

> Tài liệu trung tâm của dự án (Single Source of Truth).  
> Mọi thay đổi về kiến trúc, hành vi, dependency, cấu trúc file, trạng thái triển khai hoặc quyết định kỹ thuật **phải được cập nhật vào file này trong cùng một lần thay đổi**.

## 0. Context Capsule

Phần này giúp người hoặc AI tiếp tục dự án mà không phải quét lại toàn bộ repository.

| Trường | Giá trị hiện tại |
|---|---|
| Tên sản phẩm | OneClick Dev Server |
| Mục tiêu | Public dự án PHP/Laragon/XAMPP từ Windows hoặc Ubuntu mà không cần VPS/hosting |
| Trạng thái | `REV65_GITHUB_EMPLOYEE_DISTRIBUTION` — source sạch và một Windows EXE dành cho nhân viên được phát hành tại nhánh `main`; runtime website/database/credential/backup/cache bị loại khỏi Git |
| README revision | `65` |
| Phiên bản ứng dụng | `0.1.0-dev` |
| Nền tảng MVP | Windows x64 và Ubuntu x64 |
| Runtime ứng dụng | Wails v2.13 + Go backend + React 19/TypeScript frontend |
| Lớp cách ly | Một Ubuntu VM bảo vệ host; bên trong VM mỗi website tách bằng Linux DAC/ACL, systemd sandbox, PHP-FPM process, database account/schema và tunnel connector/firewall riêng |
| VM manager | Multipass: Hyper-V trên Windows Pro/Enterprise, QEMU trên Ubuntu |
| Windows Home | Multipass + clean VirtualBox 7.1.18 đã live-pass; backend chặn sai version, mixed MSI registration, mixed driver và pending reboot |
| Runtime trong VM | Native Nginx + PHP 8.3 FPM + MariaDB; không Docker/Compose |
| Public ingress | Một binary `cloudflared` pin checksum, một connector user/systemd service riêng cho mỗi website |
| Chế độ mặc định | Cloudflare Named Tunnel với subdomain ổn định; không dùng Quick Tunnel trong MVP hiện tại |
| Giao diện | Desktop GUI tiếng Việt là sản phẩm chính; không yêu cầu nhân viên dùng terminal |
| File đang tồn tại | Wails desktop với UI contract tự kiểm, readiness/installer, guarded Windows Multipass storage bootstrap, auto-discovery read-only, trang chi tiết riêng từng website, source manager an toàn, complete embedded WP Clean Rebuild engine, fast fresh-WordPress installer + bundled Bricks/plugin, Settings legacy-data import, JSON schema 3, shared Ubuntu lifecycle, immutable snapshot, native isolated WordPress runtime, database import, scoped backup/export và Cloudflare Named Tunnel/DNS lifecycle |
| Việc tiếp theo | User mở revision 62, giữ popup chi tiết mở trong lúc cài và xác nhận log/progress tự cập nhật khoảng 1,2 giây mà không cần đóng mở lại |

### Quy tắc tiếp tục dự án

1. Luôn đọc toàn bộ `README.md` trước khi sửa dự án.
2. Không quét toàn repository theo thói quen. Dùng **File Map** để chỉ mở các file liên quan đến tác vụ.
3. Nếu File Map thiếu, sai hoặc có dấu hiệu README không đồng bộ, chỉ khi đó mới quét có mục tiêu để sửa lại tài liệu.
4. Khi thêm/xóa/đổi trách nhiệm file, phải cập nhật **File Map**.
5. Khi thay đổi luồng xử lý, dependency, config, security invariant hoặc CLI, phải cập nhật đúng mục đặc tả tương ứng.
6. Khi hoàn thành một thay đổi, cập nhật **Current Implementation Snapshot**, **Test Matrix** và **Change Log**.
7. Không đánh dấu tính năng `DONE` nếu chưa có kiểm thử hoặc bằng chứng xác minh được ghi trong README.

README giúp tránh quét lại không cần thiết, nhưng không thay thế việc đọc file cụ thể đang được chỉnh sửa hoặc kiểm tra thay đổi chưa commit khi cần bảo vệ công việc hiện có.

---

## 1. Bài toán sản phẩm

Người dùng có một website nằm trong Laragon, XAMPP hoặc một thư mục dự án PHP trên máy cá nhân. Họ muốn:

- Chọn thư mục dự án.
- Bấm deploy trong giao diện desktop; nhân viên không phải biết hoặc nhập command.
- Nhận URL HTTPS có thể truy cập từ Internet.
- Không mở port router, không cần public IP và không thuê VPS.
- Nếu website bị cài mã độc hoặc bị chiếm quyền, thiệt hại được giới hạn trong sandbox, không trực tiếp chạm vào dữ liệu và tài khoản trên máy chính.
- Nếu máy chưa đủ dependency, ứng dụng phải phát hiện, giải thích và đề nghị cài đặt trước khi cho deploy.
- Có thể dừng, reset hoặc xoá hoàn toàn deployment.

### Không thuộc phạm vi cam kết

- Không cam kết cách ly tuyệt đối 100% trước lỗ hổng hypervisor/firmware chưa biết.
- Không biến máy cá nhân thành hạ tầng có SLA. Website dừng khi máy tắt, mất điện hoặc mất Internet.
- Không tự sửa lỗi nghiệp vụ, lỗ hổng hoặc mã độc bên trong website.
- Cloudflare Tunnel bảo vệ đường vào và ẩn origin; nó không tự làm sạch mã nguồn hay ngăn mọi lỗ hổng ứng dụng.
- MVP không hỗ trợ Windows ARM, Ubuntu ARM, macOS, Windows Server hoặc các distro Linux khác.

---

## 2. Kiến trúc chuẩn

```text
Internet
   |
   | HTTPS
   v
Cloudflare Edge
   |
   | outbound tunnel đã được VM chủ động thiết lập
   v
+---------------- OneClick Ubuntu VM -------------------------+
| cloudflared/site user -> unique 127.x origin -> shared Nginx |
|                                      |                       |
|                                      +-> PHP-FPM/site user   |
|                                          systemd sandbox     |
|                                      +-> MariaDB localhost   |
|                                          schema/user riêng   |
| Không Docker; firewall + CPU/RAM/PID/query limits             |
+---------------------------------------------------------------+
                 ^
                 | sao chép snapshot một chiều
                 |
+-------------------- Host ---------------------------+
| OneClick Desktop (Wails/Go/React) + Multipass       |
| Không mount thư mục host vào VM/runtime              |
+-----------------------------------------------------+
```

### Quyết định kiến trúc

- Không public Apache/Nginx đang chạy trực tiếp trong Laragon/XAMPP.
- Không yêu cầu Docker trên máy chính.
- `cloudflared` phải chạy trong VM, không chạy cạnh dữ liệu cá nhân trên host.
- Chỉ tạo một Ubuntu VM tên cố định `oneclick-server`; Nginx, MariaDB và package runtime được cài một lần rồi dùng chung.
- Mỗi website có working copy, Linux user, PHP-FPM systemd service, Unix socket, database schema/user, loopback origin và tunnel connector user/firewall riêng. Không website nào dùng chung credential hoặc process PHP.
- Dự án được đóng gói thành snapshot rồi sao chép vào virtual disk. Không dùng host bind mount, SMB share hoặc `multipass mount` khi runtime.
- Không cài Docker Engine, Compose hoặc containerd trong guest runtime.
- VM chỉ dùng NAT; không bridge trực tiếp vào LAN ở cấu hình mặc định.
- VM là ranh giới cứng bảo vệ máy chính. Ranh giới giữa website trong cùng VM là OS-level hardening; nếu cần chống cả guest-kernel/root/MariaDB-daemon exploit thì phải chọn chế độ VM riêng ở một phiên bản tương lai.

---

## 3. Security Invariants

Các điều dưới đây là bất biến. Thay đổi cần quyết định kiến trúc mới và phải ghi vào Decision Log.

1. **Không host mount:** website không nhìn thấy `C:\`, `/home`, profile người dùng, SSH key, browser data hoặc workspace gốc.
2. **Process riêng:** mỗi website chạy bằng Linux user và PHP-FPM service riêng; cấm root, capability, device, namespace creation và network socket IP.
3. **File riêng:** working copy/config/private directory là per-site; `wp-config.php` mode `0600`, bỏ ACL web server và kiểm tra mọi site user khác không đọc được.
4. **Database riêng:** mỗi website có schema, user và password riêng; revoke quyền cũ rồi chỉ cấp các DDL/DML WordPress cần trên đúng schema, tối đa 8 kết nối và 30 giây/statement.
5. **Tunnel riêng:** mỗi website có connector user/service/token credential riêng; firewall theo UID chỉ cho đúng loopback origin, local DNS và Cloudflare TCP 443/7844, chặn host/LAN/private/reserved ranges.
6. **Không inbound port trên host/router:** Nginx chỉ listen loopback trong guest; ingress chỉ đi qua tunnel outbound.
7. **Outbound deny-by-default:** PHP chỉ có `AF_UNIX` và `IPAddressDeny=any`; website không gọi host, LAN hoặc Internet. Chế độ tương thích sau này phải có cảnh báo và consent riêng.
8. **Least privilege:** systemd dùng `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`, `ProtectKernel*`, `RestrictNamespaces`, `CapabilityBoundingSet=` và `DevicePolicy=closed`.
9. **Resource limits:** mỗi PHP service có CPU/RAM/PID cap; MariaDB có per-user connection/query-time cap; Nginx có per-client rate/connection/timeouts; VM có CPU/RAM/disk cap.
10. **Secret isolation:** token tunnel, DB password và WordPress keys riêng theo website; không trả về frontend, argv, environment, JSON state hoặc log.
11. **Scoped elevation:** chỉ yêu cầu quyền admin/sudo lúc bật hypervisor hoặc cài dependency; desktop app hằng ngày chạy user thường.
12. **Explicit consent:** không tự cài phần mềm hệ thống, bật Windows Feature hoặc sửa host nếu người dùng chưa chấp thuận.
13. **Verified artifacts:** installer/dependency tải về phải đến nguồn cho phép, pin version và xác minh chữ ký/checksum khi nhà phát hành cung cấp.
14. **Fail closed:** quyền chéo, firewall, service policy hoặc health check không đạt thì không public website.
15. **Không tuyên bố tuyệt đối sai:** shared VM vẫn chia sẻ guest kernel, Nginx và MariaDB daemon. Lỗ hổng lấy root/kernel/daemon hoặc cạn toàn bộ disk có thể ảnh hưởng nhiều site; ranh giới tuyệt đối hơn cần một VM cho mỗi site.

### Threat model chính

| Nguy cơ | Biện pháp |
|---|---|
| Webshell đọc file cá nhân | Không mount host; VM tách khỏi máy chính |
| Ransomware mã hoá workspace | Chỉ copy snapshot vào VM; không có đường ghi ngược |
| Webshell đọc website khác | Linux user riêng, directory `0700`, ACL tối thiểu và cross-user read test |
| Website gọi vào router/NAS/máy khác | PHP không có AF_INET/AF_INET6; tunnel UID chặn private CIDR |
| Website biến thành botnet/đào coin | Không outbound + CPU/RAM/PID/query-time quotas |
| Site process escape | systemd sandbox; VM tiếp tục là security boundary bảo vệ host |
| Guest kernel/root/DB daemon exploit | Rủi ro còn lại của mô hình dùng chung; cập nhật guest hoặc chọn VM riêng khi tính năng có sẵn |
| Lộ tunnel token | Secret store riêng; redaction log; revoke khi destroy |
| DDoS hoặc request abuse | Cloudflare edge + rate limit; resource quotas trong VM |
| Supply-chain dependency | Pin version, checksum/signature, SBOM ở giai đoạn release |

---

## 4. Nền tảng và dependency

### Yêu cầu tối thiểu dự kiến cho MVP

- CPU x86-64 có VT-x/AMD-V và virtualization được bật trong BIOS/UEFI.
- RAM host: tối thiểu 8 GB; khuyến nghị 16 GB.
- Dung lượng trống: tối thiểu 15 GB cho deployment đầu tiên.
- Internet outbound HTTPS; mạng phải cho phép Cloudflare Tunnel hoạt động.
- Windows: mục tiêu Windows 10/11 x64 còn được hỗ trợ.
- Ubuntu: mục tiêu Ubuntu 22.04 LTS và 24.04 LTS x64.

Các giá trị trên là product policy ban đầu, chưa phải kết quả benchmark. Phải cập nhật sau khi có test thực tế.

### Dependency trên host

| Thành phần | Windows | Ubuntu | Bắt buộc | Ai cài |
|---|---|---|---|---|
| OneClick binary | Có | Có | Có | OneClick installer/package |
| GUI runtime | WebView2 | GTK3 + WebKit2GTK 4.1 | Có | Installer/package manager chuẩn bị trước khi mở app |
| Hardware virtualization | BIOS/UEFI | BIOS/UEFI | Có | Người dùng bật nếu đang tắt |
| Hypervisor | Hyper-V ưu tiên; VirtualBox fallback | QEMU/KVM qua Multipass | Có | Installer đề nghị cài/bật |
| Multipass | Có | Có | Có ở MVP | Installer đề nghị cài |
| Docker Desktop | Không cần | Không cần | Không | Không cài |
| `cloudflared` host | Không cần | Không cần | Không | Chạy trong guest |
| Git/Node/PHP host | Không bắt buộc | Không bắt buộc | Không | Chỉ cần nếu người dùng phát triển local |

Go, Node, npm và Wails chỉ là dependency build dành cho đội phát triển. Máy nhân viên không phải cài các công cụ này.

### Vị trí dữ liệu trên host

- Source website luôn ở nguyên thư mục do người dùng quản lý, ví dụ `D:\laragon\www\...`; OneClick chỉ đọc để tạo snapshot một chiều.
- Backup do người dùng chủ động tạo nằm tại `<thư mục ứng dụng>\data\backups\<project-id>\<YYYYMMDD-HHMMSS>`; mỗi backup gồm `source.tar.gz`, `database.sql` và `backup.json`. Dữ liệu không được upload ra dịch vụ ngoài.
- Mỗi lần người dùng bấm `Lưu source`, bản gốc của đúng file được giữ tại `<thư mục ứng dụng>\data\source-edits\<project-id>\<timestamp>` cùng `edit.json` trước khi thay file trong thư mục Laragon/XAMPP. Đây là backup cục bộ cho thao tác edit, tách với backup deployment ở trên.
- Dữ liệu của full WP Clean Rebuild engine nằm tại `<thư mục ứng dụng>\data\tools\wp-clean-rebuild\workspace`; code versioned được trích từ chính OneClick EXE vào `bundles\<hash>`, Python/runtime pin theo `uv.lock` nằm trong `runtime`, còn `sites`, `backups`, `reports`, `repairs`, `logs` và `.wpclean-cache` nằm trong `workspace`. Dữ liệu engine giữ định dạng/tên file gốc để tương thích đầy đủ, vì vậy backup/repair phải được coi là **dữ liệu không tin cậy**, không tự mở/chạy bằng Windows.
- Hồ sơ và lịch sử `Cài mới WP` nằm riêng tại `<thư mục ứng dụng>\data\tools\wp-clean-rebuild\workspace\fresh-installs`; WordPress chính thức và các ZIP shard cân bằng theo worker được cache trong `.wpclean-cache\fresh-wordpress`, không tạo lại ở lần sau nếu checksum còn đúng. Ba ZIP Bricks/child/plugin được embed trong OneClick EXE và trích vào bundle versioned, không phụ thuộc `D:\Wordpress Theme` trên máy nhân viên. FTP, database và admin password không nằm trong profile/history JSON; native credential vault là nguồn lâu dài, còn runtime secret JSON chỉ tồn tại trong thư mục `runtime\secrets` có quyền riêng và bị dọn theo lifecycle engine.
- Kho `data\recovery` của module read-only revision 37/38 được giữ nguyên nếu đã tồn tại nhưng không còn là engine hiển thị trong menu revision 39; OneClick không tự xóa hoặc tự hợp nhất kho này.
- Cài đặt có thể nhập từ một workspace `wp-clean-rebuild` cũ. Backend chỉ quét các thư mục allowlist `sites/backups/reports/repairs/logs/.wpclean-cache`, từ chối link/reparse/special/traversal, không ghi đè file xung đột, sao chép qua temp + sync + source identity check và không xóa nguồn. Password trong `sites/*.json` được đưa vào native credential vault rồi loại khỏi JSON đích.
- `state.json` nhỏ nằm trong thư mục cấu hình của user (`%LOCALAPPDATA%\OneClickDevServer` trên Windows); token Cloudflare nằm trong native credential vault.
- Một disk VM chứa Ubuntu, snapshot deploy, working copy và MariaDB data; Ubuntu image cache/disk do Multipass quản lý. Trên Windows, revision 27 cho chọn vị trí **trước deployment đầu tiên** bằng cơ chế chính thức `MULTIPASS_STORAGE`; thư mục đề xuất nằm ở `<thư mục ứng dụng>\data\multipass`.
- Backend chỉ mở action khi `state.json` có 0 project và `multipass list --format json` có 0 instance. Thư mục đích phải là đường dẫn tuyệt đối trên fixed local drive, trống, không phải drive root/junction/symlink, tách khỏi `C:\ProgramData\Multipass` và còn ít nhất 10 GB.
- Sau xác nhận riêng, chính EXE OneClick chạy helper mode có quyền quản trị để dừng dịch vụ Multipass, sao chép baseline metadata/cache khi chưa có VM nhằm giữ client authentication, ghi machine environment registry, khởi động lại và bắt buộc postflight `multipass list` sạch. PowerShell chỉ làm launcher UAC ngắn và hidden. Parent/child trao đổi bằng request/result journal schema cố định trong `%LOCALAPPDATA%\OneClickDevServer\operations\storage-*`; không phụ thuộc biến môi trường có thể mất sau UAC, không nhận command/script và giữ `result.json` để chẩn đoán. Nếu lỗi, helper bỏ registry mới và khởi động lại dịch vụ mặc định; thư mục đích được giữ để hỗ trợ chẩn đoán.
- OneClick không tự xóa kho mặc định trên ổ C và không chuyển nóng deployment. Sau khi cấu hình thành công hoặc phát sinh project/VM, vị trí bị khóa trong ứng dụng để tránh hỏng saved-state/hypervisor metadata. Ubuntu storage bootstrap chưa nối trong revision 27 và phải hiển thị `Chưa hỗ trợ`, không chạy lệnh một phần.

### Dependency bên trong guest VM

- Ubuntu LTS minimal image.
- Nginx, PHP 8.3 FPM và MariaDB từ Ubuntu 24.04 package allowlist; cài một lần dưới install lock.
- Binary `cloudflared` official pin version + SHA-256; cài một lần, mỗi website chạy connector service riêng.
- ACL/AppArmor utilities, iptables, systemd sandbox và các tool health-check tối thiểu.
- Không Docker/Compose/containerd và không có daemon socket cho website khai thác.

Guest dependencies được chuẩn bị tự động bằng image đã build sẵn và ký ở bản ổn định. Trong MVP phát triển, có thể provision bằng cloud-init; deploy chỉ được mở sau khi postflight guest thành công.

---

## 5. Preflight và cài dependency

### Nguyên tắc UX

Khi cài hoặc chạy lần đầu, ứng dụng mở **Setup Wizard** và hoàn tất readiness check trước. Nút deploy bị khoá nếu còn lỗi bắt buộc. Nhân viên chỉ thấy mô tả dễ hiểu và nút `Cài đặt`, `Sửa`, `Kiểm tra lại` hoặc `Xem hướng dẫn`; command kỹ thuật được ẩn trong phần chi tiết hỗ trợ.

Trên Windows, mọi tiến trình console do ứng dụng gọi (PowerShell, Multipass và helper kiểm tra) phải dùng chế độ hidden/no-window. Không được để cửa sổ terminal nhấp nháy. Hộp UAC của Windows vẫn phải hiển thị khi một action đã được xác nhận cần quyền quản trị.

GUI runtime là ngoại lệ phải xử lý trước khi main app có thể hiển thị:

- Windows installer bootstrap WebView2 Evergreen nếu máy chưa có.
- Ubuntu `.deb` khai báo `libgtk-3-0` và `libwebkit2gtk-4.1-0` làm package dependencies.
- Không phát hành portable build cho nhân viên trong MVP vì không thể bảo đảm dependency đã sẵn sàng.

Mỗi dependency có một trong các trạng thái:

- `READY`: đã có và phiên bản phù hợp.
- `INSTALLABLE`: thiếu nhưng ứng dụng có thể cài sau khi người dùng đồng ý.
- `USER_ACTION`: cần người dùng bật BIOS/UEFI, reboot hoặc xử lý thủ công.
- `UNSUPPORTED`: OS/CPU/edition không nằm trong phạm vi hỗ trợ.
- `BROKEN`: đã cài nhưng service/driver/health check lỗi.

### State machine

```text
APP_START
  -> acquire_single_instance_lock
  -> detect_os_arch_edition
  -> check_cpu_virtualization
  -> check_ram_disk
  -> detect_hypervisor_backend
  -> detect_multipass
  -> check_backend_service
  -> check_network_and_clock
  -> check_or_create_guest
  -> guest_postflight
  -> READY

Bất kỳ bước bắt buộc thất bại:
  -> BLOCKED
  -> hiển thị nguyên nhân + cách sửa + nút Install/Fix/Retry
```

### Logic readiness engine nội bộ

```text
function doctor():
    platform = detectPlatform()
    if platform not in supportedPlatforms:
        return UNSUPPORTED

    checks = [
        osVersionAndArchitecture,
        cpuVirtualizationCapability,
        virtualizationEnabled,
        availableMemory,
        availableDisk,
        hypervisorCompatibility,
        multipassInstalledAndVersion,
        multipassServiceHealth,
        outboundConnectivity,
        systemClockSanity,
        guestImageIntegrity,
        guestRuntimeHealth,
    ]

    run safe read-only checks
    classify each result
    never mutate host during doctor
    return structured report cho desktop UI
    deployEnabled = all required checks are READY
```

### Logic cài đặt/fix

1. Chạy `doctor` read-only.
2. Gom tất cả dependency thiếu thành một installation plan.
3. Hiển thị chính xác:
   - Thành phần sẽ được cài/bật.
   - Nguồn tải và phiên bản.
   - Dung lượng dự kiến.
   - Có cần admin/sudo hoặc reboot không.
4. Chờ người dùng xác nhận.
5. Chỉ elevate tiến trình helper cho action cần quyền cao.
6. Tải artifact vào thư mục tạm riêng.
7. Xác minh signature/checksum.
8. Cài từng dependency; ghi log đã redaction.
9. Nếu cần reboot, lưu resume token và tiếp tục postflight sau reboot.
10. Chạy lại `doctor`; chỉ báo thành công khi toàn bộ required checks là `READY`.

### Hành vi theo hệ điều hành

#### Windows

1. Kiểm tra Windows edition/version, x64 và virtualization firmware flag.
2. Nếu Windows Pro/Enterprise và Hyper-V chưa bật:
   - Đề nghị bật các Windows Features cần thiết.
   - Giải thích việc bật có thể cần admin và reboot.
3. Kiểm tra/cài Multipass từ nguồn chính thức.
4. Xác minh Multipass đang dùng backend mong muốn và có thể launch một VM health-check.
5. Windows Home không được giả vờ hỗ trợ Hyper-V. Khi Multipass báo driver `virtualbox`, readiness bắt buộc phải tìm và chạy được `VBoxManage.exe`; nếu thiếu thì chặn deploy và đề nghị cài VirtualBox.

Trạng thái code hiện tại: nút `Cài đặt/Sửa cài đặt` tải Multipass `1.16.3` từ release GitHub chính thức của `canonical/multipass`, chỉ cho phép HTTPS tới host GitHub allowlist, xác minh Windows Authenticode có trạng thái `Valid` và signer chứa `Canonical`, rồi mới chạy `msiexec` qua UAC. Backend `virtualbox` chỉ được readiness chấp nhận khi `VBoxManage --version` bắt đầu bằng exact `7.1.18r`; Windows uninstall registry phải có đúng một `Oracle VirtualBox 7.1.18`; bốn driver host phải cùng version và không còn pending replacement. Action VirtualBox tải gói Oracle official, pin SHA-256 `de14e4d6572e5a602e5053f3fd8c641356fd377ef9baa813136ae32711d19488`, kiểm Authenticode Oracle, rồi trong một UAC ẩn sẽ stop Multipass, gỡ mọi Oracle VirtualBox MSI registration xung đột, chuyển install directory còn sót vào recovery path, cài sạch 7.1.18 và chỉ xóa recovery sau khi installer pass. PowerShell signature check cô lập `PSModulePath`; temp/result/checksum được nhúng an toàn vào encoded command; progress pulse 5 giây, terminal ẩn và sau cài luôn yêu cầu restart. App không tự sửa EFI/BIOS của VM; compatibility fail closed trước mutation.

#### Ubuntu

1. Kiểm tra distro/version, x86-64, `/dev/kvm` và quyền truy cập KVM.
2. Kiểm tra/cài `snapd` nếu thực sự cần và được người dùng đồng ý.
3. Kiểm tra/cài Multipass từ kênh chính thức đã pin.
4. Xác minh daemon và launch một VM health-check bằng QEMU/KVM backend.

Trạng thái code hiện tại: action Multipass dùng `snap install multipass --channel=1.16/stable`, yêu cầu quyền qua `pkexec` khi app không chạy root. Luồng này đã code để cross-platform nhưng chưa build/test trên Ubuntu.

### Không được làm

- Không tự động chạy installer ngay khi mở ứng dụng.
- Không che giấu thành phần, nguồn tải, quyền hoặc ảnh hưởng của action hệ thống; command chi tiết chỉ hiện trong vùng hỗ trợ kỹ thuật, không bật terminal cho nhân viên.
- Không tiếp tục deploy khi virtualization đang tắt.
- Không chạy website trực tiếp trên host Ubuntu để “cho chạy được”; native runtime vẫn phải nằm trong OneClick VM.
- Không yêu cầu cài Docker Desktop vì kiến trúc không cần nó.

---

## 6. Luồng deploy

### Các bước chuẩn

```text
SELECT_PROJECT
  -> validate_project_path
  -> detect_project_type
  -> load_or_create_oneclick_config
  -> security_scan_metadata (không tuyên bố diệt malware)
  -> build_copy_manifest
  -> create_immutable_snapshot_archive
  -> ensure_shared_oneclick_server
  -> transfer_archive_to_vm
  -> install_native_packages_once
  -> create_site_user_secrets_database_and_php_sandbox
  -> apply_acl_systemd_and_resource_policy
  -> local_health_check_in_vm
  -> detect_local_wordpress_database_or_select_sql_dump
  -> export_or_copy_database_one_way
  -> replace_and_verify_project_database_in_vm
  -> verify_cloudflare_zone_and_nameservers
  -> create_named_tunnel_and_managed_cname
  -> apply_tunnel_egress_firewall_before_connector
  -> start_cloudflared
  -> external_health_check
  -> publish_url
```

Nếu bất kỳ security policy hoặc health check bắt buộc nào thất bại, tunnel không được public và tiến trình chuyển sang `FAILED_SAFE`.

### Bước `Tạo môi trường` đang hoạt động

Đây là bước chuẩn bị VM trước deploy; chưa tạo snapshot, chưa chuyển và chưa chạy mã website.

1. UI gửi `projectPath` và tên hiển thị để xin plan; backend tự kiểm tra lại thư mục bằng project detector.
2. Backend luôn dùng fixed instance `oneclick-server`; frontend không được chọn instance name. Mọi project record hợp lệ cùng tham chiếu server này.
3. Backend kiểm tra daemon bằng cách lặp `multipass list --format json` mỗi 2 giây (plan tối đa 30 giây, create tối đa 60 giây). Không dùng `wait-ready` vì Multipass 1.16.3 được pin không có command này.
4. Nếu chưa có instance, backend chạy allowlist tương đương `multipass launch 24.04` với `--cpus 2 --memory 4G --disk 40G --timeout 900`, fixed cloud-init marker `owner=oneclick`, `image=24.04`, `role=shared-server`, UTC, tắt package update/upgrade, không `--mount`, không bridge/network tuỳ chỉnh. Trong khi CLI chạy, app poll trạng thái và probe `multipass exec true`; SSH là authority dù `list/info` vẫn trả IPv4 `N/A`.
5. Nếu đã có instance cùng tên, backend khởi động khi cần và chỉ dùng lại sau khi đọc đủ ba marker. Fresh instance còn phải có `/var/lib/cloud/instance/boot-finished` trước khi báo sẵn sàng.
6. Health-check trong guest dùng `multipass exec --no-map-working-directory`: xác minh marker, Ubuntu 24.04 và `x86_64/amd64`.
7. `multipass info --format json` phải báo `Running`, tối thiểu 2 CPU, 4 GB RAM, 40 GB disk và `mounts` rỗng; sai điều kiện thì fail closed, không tự xoá instance.
8. UI nhận event `environment:progress`; thành công hiển thị VM/IP và bước kế tiếp `Sao chép website`.

Một mutex trong process chặn hai thao tác tạo VM đồng thời. Tổng timeout là 20 phút; launch/start/reset có tiến trình pulse 5 giây/lần. `N/A` không phải IP nhưng cũng không còn là kết luận mất mạng: `multipass exec`/SSH là tín hiệu quyết định. Nếu instance đã ở `Starting`, app không gọi `start` chồng lên. Shared server chỉ được `Tạo lại từ đầu` khi state toàn hệ thống chưa có snapshot/runtime/database/tunnel; có dữ liệu của bất kỳ website nào thì backend từ chối xoá để bảo vệ tất cả project. Cloud-init tạm được tạo quyền hạn chế và xoá sau launch. Child process dùng `internal/hostexec`, nên Windows không bật console popup.

Parser `multipass info` hỗ trợ JSON 1.16.3 dạng `{errors, info: {name: object}}` và `cpu_count` dạng string/number. Output CLI loại ANSI/control và chuỗi spinner dài trước khi ghi lỗi/lịch sử.

### Nhận diện dự án MVP

| Dấu hiệu | Loại dự án | Document root mặc định |
|---|---|---|
| `artisan` + `composer.json` | Laravel | `public/` |
| `wp-config.php` hoặc `wp-config-sample.php` | WordPress | thư mục gốc |
| `composer.json` + `public/index.php` | PHP framework | `public/` |
| `index.php` | Generic PHP | thư mục gốc |
| Chỉ HTML/CSS/JS | Static site | thư mục gốc |

Nếu có nhiều kết quả hoặc document root không chắc chắn, yêu cầu người dùng xác nhận. Không đoán im lặng.

Trạng thái code hiện tại: `internal/project` nhận diện theo bảng trên bằng cách chỉ đọc các file metadata/entrypoint đã biết ở thư mục gốc và `public/`; không recurse, không chạy code dự án và không sửa source. Khi app mở, detector tự kiểm tra **một cấp thư mục con** trong các web-root conventional trên fixed drive (`laragon\www`, `xampp\htdocs`, `wamp64\www`); Linux dùng `/var/www/html`, `/srv/www` và `~/Sites`. Tối đa 128 website hợp lệ được trả về, sắp xếp/deduplicate theo canonical path. Kết quả chỉ hiển thị `Chưa thiết lập`, chưa ghi JSON cho tới khi người dùng bấm `Thiết lập` và xác nhận cấu hình. Folder picker vẫn dùng được cho vị trí khác. Backend kiểm tra lại path/document root rồi lưu stage/status/history vào `state.json`; không ghi `oneclick.yaml` vào project. Document root tuyệt đối hoặc thoát bằng `..` bị backend từ chối trước khi lưu và trước VM plan.

### PHP version

Thứ tự chọn:

1. Giá trị `runtime.php` trong `oneclick.yaml`.
2. Ràng buộc rõ ràng trong `composer.json`.
3. Metadata do adapter Laragon/XAMPP cung cấp nếu có.
4. Yêu cầu người dùng chọn từ danh sách phiên bản được hỗ trợ.

Không tự dùng “latest” cho production vì có thể làm hỏng ứng dụng.

### Copy manifest

Mặc định không sao chép:

- `.git/`
- `.idea/`, `.vscode/`
- `node_modules/`, file log/cache/session/view local có thể tái tạo
- SSH key, certificate cá nhân hoặc backup ngoài project
- database dump `.sql`, `.sql.gz`, `.dump` và file backup phổ biến
- file khớp secret denylist toàn cục như `.env`, `wp-config.php`, `.npmrc`, `.netrc`, `auth.json`, key/certificate

Chính sách secret MVP:

- Không sao chép `.env`; chỉ cho phép template `.env.example`, `.env.sample`, `.env.dist` để runtime tạo cấu hình mới ở bước sau.
- Luôn thay DB host/password bằng credential của deployment.
- Không in nội dung `.env` ra log.

Giới hạn snapshot MVP: tối đa `100.000` file, tổng `4 GB`, mỗi file `512 MB`. Chỉ nhận regular file. Symlink/junction/reparse point trong vùng được copy bị từ chối thay vì follow; đường dẫn tuyệt đối hoặc traversal bị từ chối. Source được mở read-only, kiểm tra lại identity/size/mtime trong lúc đóng gói và không thực thi.

### Bước `Sao chép website` đang hoạt động

1. CTA chỉ xuất hiện khi state của project có shared VM `environment_ready` hoặc snapshot cũ có thể làm mới.
2. `GetCopyPlan` đọc metadata để đếm file/dung lượng/exclusion; chưa tạo archive và không in tên/nội dung file lên GUI.
3. UI hiển thị một primary CTA, số file, dung lượng, số mục loại trừ và số secret bị chặn.
4. Backend kiểm tra lại project config, deterministic VM name và state JSON; thao tác song song trên cùng project bị state `copying` chặn.
5. Builder tạo `tar.gz` tạm quyền `0600`, thứ tự file ổn định, mode chuẩn hoá, per-file SHA-256 trong `.oneclick-manifest.json`, rồi tính SHA-256 toàn archive.
6. Manifest không chứa timestamp biến đổi, nên source không đổi tạo cùng checksum/snapshot ID; retry có thể reuse snapshot bất biến.
7. Trước transfer, backend kiểm tra lại ownership marker, Ubuntu 24.04, x86-64, state `Running` và `mounts` rỗng.
8. Archive được chuyển bằng `multipass transfer` vào home guest; không dùng `multipass mount`, SMB hoặc bind mount.
9. Guest chạy `sha256sum`; chỉ khi khớp mới extract bằng `tar --no-same-owner --no-same-permissions` vào staging path do backend sinh từ token hex.
10. Snapshot có marker checksum, bị `chmod -R a-w`, rồi atomic move vào `/var/lib/oneclick/deployments/<project-id>/<snapshot-id>`.
11. Symlink `current` nằm hoàn toàn trong guest và được xác minh lại; source host không có đường ghi ngược.
12. Archive/staging tạm được dọn; state JSON chỉ lưu ID, checksum, file count, bytes, guest path và lịch sử, không lưu source/secret.

UI nhận event `copy:progress`; app đóng/mất điện ở state `copying` được phục hồi thành `copy_failed` với CTA thử lại. Kết quả thành công hiển thị bước kế tiếp `Cài môi trường chạy`.
Nếu cấu hình project thay đổi sau khi đã copy, backend giữ shared VM và dữ liệu website khác, nhưng hạ project này về `environment_ready`, xoá metadata snapshot cũ khỏi state và yêu cầu sao chép lại. Không cho sửa cấu hình khi environment/copy operation đang chạy.

### Bước `Cài môi trường chạy` đang hoạt động

1. CTA chỉ xuất hiện ở `source_ready`, `runtime_failed` hoặc `runtime_ready`; frontend chỉ gửi `projectPath`, còn backend đọc VM/snapshot/project identity từ JSON state. `runtime_ready` bây giờ đi tới bước database, không đi thẳng tới domain.
2. Plan xác nhận PHP 8.3 FPM + Nginx + MariaDB native, hai thành phần per-site, package dùng chung, resource cap, mạng nội bộ và trạng thái `chưa public`; không Docker/Compose/image pull.
3. Version lock schema 2 được embed trong binary với revision `native-v2` và allowlist package Ubuntu 24.04. Lock từ chối `docker.io`, `docker-compose-v2`, `containerd` và package ngoài danh sách.
4. Backend xác minh fixed VM name, marker shared-server, Ubuntu 24.04 x64, state `Running`, mounts rỗng, immutable snapshot/current-link/checksum trước mutation.
5. Phase `install` giữ guest file lock, chỉ apt install khi package/revision thiếu. Nginx, MariaDB, PHP và cloudflared binary được dùng chung nên website thứ hai không cài Ubuntu/runtime lại.
6. Phase `configure` tạo site user `oc<id>`, directory `0700`, working copy riêng `/var/lib/oneclick/sites/<id>/current`, private temp/session, DB/schema/password và tám WordPress key/salt riêng. Snapshot bất biến không bị sửa.
7. Nginx dùng một listener loopback 127.x riêng theo project, deny dotfile/wp-config/upload PHP, security headers, timeout và per-client rate/connection limit. Không published port ra host/LAN.
8. Mỗi PHP-FPM chạy bằng systemd service riêng và Unix socket riêng; chỉ `AF_UNIX`, `IPAddressDeny=any`, capability rỗng, no-new-privileges, private tmp/device, strict filesystem/kernel protection, CPU 75%, RAM 512 MB và 128 task. Master/worker log vào file 0600 trong private directory của site; config được test bằng chính site user trước start. Runtime directory dùng private group + ACL mask `--x` chỉ cho Nginx đi tới socket; restart lỗi bị giới hạn 3 lần/phút. `open_basedir` chỉ gồm site/private/temp; file editing/modification WordPress và hàm process execution bị tắt.
9. MariaDB chỉ bind `127.0.0.1`, tắt local infile/symlink/file import. Site user bị revoke toàn bộ quyền cũ rồi chỉ nhận SELECT/INSERT/UPDATE/DELETE/CREATE/DROP/ALTER/INDEX/temp-table/lock-table trên đúng schema, tối đa 8 kết nối và 30 giây mỗi statement.
10. Phase `verify` kiểm tra systemd policy, socket ACL, `wp-config.php` 0600 không có ACL Nginx, mọi site user khác không đọc được, DB không có global/cross-schema privilege, account limit đúng và HTTP nội bộ pass. Bất kỳ điều kiện nào sai chuyển `runtime_failed` và không public.

App đóng ở `runtime_installing` được phục hồi thành `runtime_failed` và có CTA thử lại. Installer idempotent với cùng snapshot, giữ secret/database của website hiện tại và package cache dùng chung. Mỗi phase có timeout riêng: install 12 phút, configure 6 phút, start 3 phút, verify 5 phút; progress tiếp tục pulse nhưng context timeout sẽ dừng CLI thay vì giữ GUI vô hạn.

### Bước `Sao chép database` đang hoạt động

1. Đây là bước riêng sau `runtime_ready`: `runtime_ready -> database_importing -> database_ready | database_failed`. Lỗi/retry database không cài lại Ubuntu, Nginx, PHP hoặc MariaDB. Chỉ `database_ready` mới được chọn domain; dừng tunnel trả lại `database_ready`.
2. Mặc định `Tự lấy từ WordPress`: backend chỉ đọc tối đa 1 MB `wp-config.php`, không chạy PHP/source, kiểm tra identity/size/mtime trong lúc đọc, và chỉ chấp nhận giá trị chuỗi trực tiếp của `DB_NAME`, `DB_USER`, `DB_PASSWORD`, `DB_HOST`, `$table_prefix`. Mật khẩu rỗng của Laragon local được chấp nhận khi hằng `DB_PASSWORD` tồn tại rõ ràng.
3. Auto export chỉ cho DB host cục bộ `localhost`, `127.0.0.1`, `::1` với TCP port hợp lệ. Remote/LAN DB bị từ chối và UI hướng dẫn chọn file. Công cụ dump chỉ lấy từ đúng cây cài Laragon/XAMPP chứa project hoặc PATH; không quét toàn ổ.
4. Trước `mysqldump`, backend probe đúng loopback host/port. Nếu database đang tắt và project nằm trong cây Laragon/XAMPP tin cậy, OneClick chỉ dùng `mysqld/mariadbd` cùng bộ với dump tool và regular `my.ini`, chạy ẩn không terminal, serialize thao tác start và chờ tối đa 75 giây. Server do OneClick tự bật bị ép `bind-address=127.0.0.1`; MySQL 8 Laragon còn tắt MySQL X. Không tìm thấy launcher/config đúng thì fail closed và hướng dẫn bật database, không thử command từ source/frontend.
5. Credential export nằm trong option file tạm `0600` dưới `<application-root>/data/transfers`, luôn là argument đầu `--defaults-extra-file`; password không nằm trong command, environment, frontend, state hoặc log. Dump dùng single-transaction/quick/skip-lock/hex-blob và được SHA-256.
6. Cách dự phòng cho phép người dùng chọn regular file `.sql`/`.sql.gz` tối đa 4 GB; symlink/reparse và file trống bị từ chối. App kiểm tra lại file identity/size/mtime trong lúc copy, tạo bản sao tạm rồi transfer một chiều; file `.gz` được stream, không bung plaintext trên host/guest.
7. Guest xác minh exact project ID/path, checksum, gzip và table prefix. Local MariaDB root socket chỉ drop/recreate schema `oc_<project-id>` rồi revoke/regrant đúng privilege/connection/query limit; nội dung dump được import bằng site DB user, không phải root. Sau import phải có bảng và đúng `<prefix>options` mới thành công.
8. `wp-config.php` trong mutable working copy được cập nhật table prefix sau import. Trước khởi động tunnel, OneClick cập nhật `home/siteurl` trong đúng options table sang hostname HTTPS đã chọn; lỗi sẽ rollback resource Cloudflare và không public.
9. Dump/script tạm được dọn cả khi lỗi. Source DB trên host chỉ được đọc và không bị sửa. Database đích là schema riêng trong shared MariaDB, không bind port ra host/LAN và credential chỉ thuộc website đó.
10. State schema 3 chỉ lưu `databaseState`, nhãn nguồn, số bảng, số byte, table prefix và thời điểm import; không lưu DB name/user/password, dump path hoặc nội dung SQL. App đóng ở `database_importing` được phục hồi thành `database_failed` với CTA thử lại.

### Quản lý Source và Database đã deploy

1. Mỗi trang chi tiết website có khối `Source và database`; source gốc và working copy trong Ubuntu được phân biệt rõ. `Mở source` chỉ mở exact project path đã lưu bằng file manager, không truyền command và không mount working copy ra Windows.
2. `Tạo backup` được phép khi runtime/database healthy ở `database_ready`, `tunnel_failed`, `public` hoặc `tunnel_stop_failed`. Backup đọc đúng working copy và schema riêng của project trong shared Ubuntu.
3. Reverse transfer bị giới hạn cứng vào `/home/ubuntu/.oneclick/exports/<project-id>/<timestamp>/{source.tar.gz,database.sql}` và chỉ ghi vào `<application-root>/data/backups/<project-id>/<timestamp>`; không có API tải arbitrary guest file.
4. Guest từ chối symlink/special file, loại `wp-config.php` và `.env*`, dump bằng exact per-site `database.cnf`, rồi xuất SHA-256/byte count. Host bắt buộc kiểm tra regular file, checksum, size, tar traversal/type/secret trước khi atomic commit `backup.json`.
5. Cảnh báo SFTP `cannot set permissions for local file` chỉ được bỏ qua trên Windows sau khi file NTFS thực sự tồn tại; regular-file và SHA-256 postflight vẫn bắt buộc. Mọi lỗi transfer khác fail closed và xóa thư mục backup chưa hoàn chỉnh.
6. Người dùng có thể mở/chỉnh `database.sql`, nhưng chỉ được thay database sau khi website đã dừng public và có ít nhất một backup hoàn chỉnh. Backend tái kiểm tra điều kiện này; import đầu tiên ở `runtime_ready/database_failed` không bị ảnh hưởng.
7. Revision 35 chưa ghi ngược source đã deploy về source gốc và chưa áp dụng thay đổi source gốc vào working copy. Pha kế tiếp sẽ thêm `Cập nhật website` theo snapshot mới, backup trước, health-check và rollback; nguyên tắc không host mount vẫn giữ nguyên.

---

## 7. Tunnel modes

### `quick` — chưa dùng trong MVP hiện tại

- Dành cho preview/dev.
- Không cần Cloudflare account/domain.
- Nhận URL ngẫu nhiên `*.trycloudflare.com`.
- Không coi là production, không SLA, URL có thể thay đổi.

### `named` — luồng mặc định hiện tại

- Dành cho URL ổn định.
- Có thể lưu nhiều Cloudflare zone; mỗi zone dùng full setup/nameserver Cloudflare và scoped API token riêng có `Zone Read`, `DNS Write`, `Cloudflare Tunnel Write`.
- Khi xuất bản, người dùng chọn một zone đang `Sẵn sàng` và nhập subdomain. Backend ràng buộc hostname vào đúng zone đã chọn, ưu tiên exact/longest suffix match và lưu `domainZone` theo project để stop/recovery dùng đúng credential.
- API token người dùng dán chỉ tồn tại tạm trong bộ nhớ form/Wails request, backend không bao giờ trả ngược token; bản lưu lâu dài chỉ nằm trong Windows Credential Manager/Linux Secret Service. Connector token được chuyển bằng temporary regular file, cài `0400 root:root` và cấp cho đúng systemd service bằng `LoadCredential`; `cloudflared tunnel run --token-file` đọc credential runtime, token không nằm trong command, environment, repo hoặc `state.json`.
- OneClick tạo một remotely-managed tunnel và CNAME proxied cho hostname đã xác nhận; không ghi đè DNS record không thuộc đúng project.
- Binary official `cloudflared` 2026.8.2 chỉ tải một lần, bắt buộc SHA-256 trong embedded lock trước khi cài. Mỗi website có connector UID/service/token/firewall chain riêng; PHP site không dùng process hay egress của tunnel.
- Dừng truy cập xóa DNS/tunnel, service credential và firewall guest nhưng giữ runtime, source snapshot và database.

### `private`

- Named Tunnel + Cloudflare Access.
- Mặc định deny; chỉ email/group/policy được phép mới truy cập.
- Phù hợp demo nội bộ, trang admin hoặc dự án chưa công khai.

---

## 8. Runtime policy

### Website sandbox mặc định

- Mỗi website có non-root Linux user, primary group, source/private/config directory và PHP-FPM systemd service riêng.
- PHP service không có IP network family, capability, device hoặc quyền ghi ngoài site/private directory; systemd bảo vệ home, system/kernel/control-group/proc/tmp.
- Nginx dùng chung chỉ có read/traverse ACL trên source cần serve; không đọc được `wp-config.php`.
- PHP socket là Unix socket mode `0600`, chỉ thêm ACL cho Nginx; không listen TCP.
- MariaDB dùng chung chỉ nhận local socket/loopback; mỗi account chỉ có quyền trên schema tương ứng và bị giới hạn kết nối/thời gian statement.
- Tunnel connector là process riêng, không chạy cùng UID với PHP; token qua systemd credential và firewall egress theo UID.
- Health/policy/cross-site test bắt buộc trước `runtime_ready`/`public`.

### Resource mặc định ban đầu

| Tài nguyên | Giá trị dự kiến | Ghi chú |
|---|---:|---|
| VM CPU | 2 vCPU | Có thể cấu hình |
| VM RAM | 4 GB | Dùng chung cho runtime nhiều website |
| VM disk | 40 GB | Dynamic allocation; storage nên đặt ở ổ còn dung lượng trước deployment đầu |
| PHP/site | 75% một CPU, 512 MB, 128 tasks | systemd hard cap riêng từng website |
| DB user/site | 8 kết nối, 30 giây/statement | Giảm noisy-neighbor và query treo |
| Tunnel/site | 30% một CPU, 160 MB, 64 tasks | Connector process riêng |
| Nginx/client | 30 request/s, burst 60, 20 connection | Kèm header/body/send timeout |

Các giới hạn trên đã `IMPLEMENTED_NOT_LOAD_TESTED`. Shared filesystem/MariaDB chưa có hard per-site disk quota; đây là residual noisy-neighbor risk phải bổ sung trước production multi-tenant không tin cậy.

### Egress profiles

- `strict` (mặc định hiện tại): PHP không có IP networking; chỉ connector UID được dùng local DNS, đúng origin và Cloudflare TCP 443/7844.
- `web-compatible`: cho HTTPS outbound nhưng vẫn chặn private/LAN/metadata ranges; hiển thị cảnh báo exfiltration.
- `build`: mở tạm các registry/package domains được pin trong giai đoạn build, sau đó tự quay về `strict`.

---

## 9. Lifecycle và phục hồi

| Nút/hành động trong GUI | Ý nghĩa |
|---|---|
| `Kiểm tra lại` | Kiểm tra máy và dependency, không thay đổi hệ thống |
| `Cài đặt/Sửa` | Hiển thị plan rồi cài dependency sau xác nhận |
| `Chọn website` | Mở folder picker native, nhận diện dự án và tạo cấu hình |
| `Xuất bản` | Copy snapshot, tạo site sandbox/database và public tunnel trong shared VM |
| Trang chi tiết | Trạng thái VM, site service, database, tunnel, URL và log đã redaction |
| `Dừng truy cập` | Dừng/xoá tunnel route; giữ site runtime/database để chạy lại |
| `Làm mới website` | Chỉ thay working copy/runtime của đúng project từ snapshot sạch |
| `Xoá website` | Dừng tunnel và chỉ xoá user/service/schema/source của project; không xoá shared VM |

### Trạng thái deployment

```text
NEW -> PREFLIGHT -> PREPARING -> STARTING -> HEALTH_CHECK
    -> PUBLIC | PRIVATE

Mọi trạng thái có thể đi tới:
    FAILED_SAFE -> STOPPED -> RESETTING -> STARTING
                         \-> DESTROYED
```

### Crash recovery

- Mọi operation có ID và journal nhỏ trên host.
- Lệnh phải idempotent hoặc có bước rollback rõ ràng.
- App khởi động lại sẽ phát hiện operation dang dở, kiểm tra trạng thái thật rồi đề nghị resume/rollback.
- Không public tunnel nếu app chết giữa lúc firewall chưa áp dụng xong.

Trạng thái hiện tại revision 31: cấu hình/lịch sử project đã persist; các operation dang dở `environment_creating`, `copying`, `runtime_installing`, `database_importing`, `tunnel_starting`, `tunnel_stopping` được chuyển sang failure stage tương ứng với CTA thử lại. Shared VM recreate bị backend từ chối khi bất kỳ project nào đã có snapshot/runtime/database/tunnel. Per-site destroy và hard disk quota vẫn nằm ở roadmap.

---

## 10. Cấu hình dự án dự kiến

File `oneclick.yaml` nằm trong project nhưng không chứa secret:

```yaml
schema: 1
name: my-site
type: laravel
document_root: public

runtime:
  php: "8.3"
  web: nginx

database:
  engine: mysql
  version: "8.0"
  import: null

tunnel:
  mode: quick
  hostname: null
  access: public

security:
  profile: strict
  outbound_allow_domains: []
  writable_paths:
    - storage
    - bootstrap/cache

resources:
  cpus: 2
  memory_mb: 2048
  disk_gb: 10
```

Parser phải từ chối schema không hỗ trợ và unknown security-sensitive fields; không âm thầm bỏ qua cấu hình an toàn bị viết sai.

---

## 11. Desktop UI và error contract

### Đối tượng và nguyên tắc

- Đối tượng chính là nhân viên không có kiến thức terminal, Docker, VM hoặc networking.
- Tiếng Việt là ngôn ngữ mặc định của MVP; tiếng Anh bổ sung sau.
- Mỗi màn hình có tối đa một primary CTA; tùy chọn nâng cao dùng progressive disclosure.
- Không hiển thị command trong luồng nhân viên. `Chi tiết kỹ thuật` mặc định thu gọn và dùng cho IT/support.
- Status luôn có icon + nhãn + mô tả, không truyền đạt chỉ bằng màu.
- Desktop control tối thiểu 36 px để giữ mật độ CRM; nếu có giao diện touch phải nâng target lên tối thiểu 44 px. Keyboard navigation và focus ring phải rõ ràng.
- Text contrast tối thiểu WCAG AA 4.5:1; hỗ trợ `prefers-reduced-motion`.
- Animation chỉ dùng cho feedback/progress, 150–300 ms và không làm layout nhảy.

### Design system MVP

- Phong cách: CRM truyền thống gọn, nền sáng, border rõ, mật độ cao vừa phải; ưu tiên bảng trạng thái và hành động.
- Font: Inter nếu được bundle, fallback `Segoe UI`/system sans; không tải font từ Internet lúc runtime.
- Primary: blue; success: green; warning: amber; destructive: red; nền slate rất nhạt.
- Icon: SVG outline nhất quán; không dùng emoji làm icon cấu trúc.
- Light mode là mode chính của MVP; dark mode chỉ thêm sau khi kiểm tra contrast độc lập.
- Cỡ chữ hiển thị tối thiểu 14 px. Mọi action button chính/phụ/tertiary và icon button cao 36 px; `npm run check:ui` chặn tái phạm typography, chiều cao button, focus và reduced-motion trong mọi frontend build.
- Trang danh sách và module Khôi phục dùng toàn bộ chiều rộng cột nội dung; chỉ nội dung đọc dài hoặc form cần giới hạn độ dài dòng mới được giữ readable measure riêng.
- Sidebar chiếm một cột cố định theo viewport; `html/body/#root` không cuộn, chỉ `.main-content` cuộn dọc và chặn tràn ngang. Khi nội dung dài, logo/menu/footer sidebar phải giữ nguyên vị trí.
- Modal giữ focus bên trong, hỗ trợ `Escape` khi không có operation đang chạy và trả focus về control trước đó khi đóng.

### Screen map

1. **Kiểm tra máy/Setup Wizard:** tóm tắt readiness, bảng dependency compact, plan xác nhận và tiến trình cài.
2. **Tổng quan:** trạng thái bảo vệ và website gần đây.
3. **Website — danh sách:** tóm tắt, website đã lưu, auto-discovery và hành động `Chi tiết/Thiết lập`; không đặt quy trình deploy nối dài bên dưới danh sách phát hiện.
4. **Domain:** danh sách nhiều Cloudflare zone, trạng thái DNS/token, thêm/cập nhật/xóa có xác nhận và mở Cloudflare.
5. **Deploy Wizard:** nhận diện dự án → cấu hình website → tạo môi trường → cấu hình DB → chọn domain/subdomain → xác nhận → progress.
6. **Chi tiết website:** thông tin website, tiến trình Ubuntu → Source → Vùng chạy → Database → Domain, đúng một hành động tiếp theo và lịch sử của riêng website; có nút quay lại danh sách rõ ràng.
7. **Cài đặt/Hỗ trợ:** nơi lưu dữ liệu máy ảo, system report, repair, export diagnostic và update; không chứa quản lý domain.

### Error contract

- Mọi lỗi hiển thị bằng tiếng Việt theo cấu trúc: chuyện gì xảy ra, ảnh hưởng gì, người dùng bấm gì để sửa.
- Technical detail, error code và log path nằm trong vùng thu gọn.
- Error code ổn định theo nhóm:
  - `10xx`: platform/preflight.
  - `20xx`: dependency/install.
  - `30xx`: project detection/config.
  - `40xx`: VM/runtime.
  - `50xx`: tunnel/auth/DNS.
  - `60xx`: security policy.
- Mỗi lỗi phải có: mã, nguyên nhân ngắn, tác động, hành động sửa và đường dẫn log redacted.
- Không in command chứa token/password.

---

## 12. Update và supply-chain

- OneClick update không được tự chạy binary mới mà không xác minh chữ ký.
- Guest image có version, digest và compatibility range với OneClick binary.
- Dependency versions nằm trong một lock manifest của repo; README chỉ ghi vai trò và policy, không lặp toàn bộ hash dài.
- Update guest dùng replace/recreate từ image sạch thay vì sửa thủ công không truy vết.
- Trước update có thay đổi data format, tạo backup/snapshot và có rollback plan.
- Release tạo SBOM và checksum cho Windows/Linux artifacts.

---

## 13. Dữ liệu và quyền riêng tư

- Project archive, VM disk và backup chỉ lưu local trừ khi người dùng chủ động chọn dịch vụ ngoài.
- Lịch sử giao diện lưu local dạng JSON schema `3`: Windows tại `%LOCALAPPDATA%\OneClickDevServer\state.json`; Ubuntu tại thư mục cấu hình người dùng do hệ điều hành trả về, dưới `OneClickDevServer/state.json`. Loader tự migrate schema 1 (một Cloudflare connection) và schema 2 (multi-domain chưa có database state), đồng thời vẫn từ chối schema lạ.
- `state.json` chỉ chứa danh sách zone không bí mật, tên/path project, cấu hình không bí mật, bước/status, tên/trạng thái/IP VM, metadata snapshot/runtime/database không bí mật, `domainZone`, tunnel/DNS ID, public URL và tối đa 50 sự kiện gần nhất mỗi project; không lưu DB credential/name, dump path/nội dung, password, API/connector token, cookie hoặc nội dung website.
- Backend kiểm tra lại path/document root, từ chối unknown JSON fields và file lớn hơn 5 MB. File được ghi qua temporary file rồi replace nguyên tử; không ghi lịch sử vào source project.
- Cloudflare xử lý traffic tunnel theo chế độ người dùng chọn.
- Telemetry mặc định tắt trong MVP.
- Nếu bổ sung telemetry, phải opt-in và công bố chính xác trường dữ liệu.
- Log phải lọc token, cookie, Authorization header, DB URL/password và biến môi trường nhạy cảm.

---

## 14. File Map

### File hiện có

| File | Trạng thái | Trách nhiệm |
|---|---|---|
| `README.md` | ACTIVE | Nguồn sự thật trung tâm, toàn bộ logic và trạng thái dự án |
| `.gitignore` | ACTIVE | Loại toàn bộ runtime/user data, website/database/backup/report/cache/diagnostics/secret/dependency khỏi source control; chỉ cho phép `data/go.mod` và đúng một Windows EXE dành cho nhân viên trong `build/bin` |
| `data/go.mod` | ACTIVE BUILD BOUNDARY | Nested module rỗng để Go/Wails không recurse vào kho Multipass có ACL hệ thống; không chứa hoặc điều khiển dữ liệu VM |
| `main.go` | ACTIVE | Wails desktop entrypoint, window và asset configuration |
| `app.go` | ACTIVE | API bridge giữa React UI và Go backend |
| `app_test.go` | ACTIVE TEST | Chọn Cloudflare zone theo explicit/longest suffix, từ chối zone không khớp và shared-server recreate data guard |
| `go.mod`, `go.sum` | ACTIVE | Go module và dependency lock |
| `wails.json` | ACTIVE | Wails build/frontend/package configuration |
| `internal/readiness/` | ACTIVE | Read-only platform, virtualization, resource, Multipass và network checks |
| `internal/installer/` | ACTIVE | Allowlisted install plan, progress/result contract, Multipass Windows/Ubuntu actions và artifact verification |
| `internal/hostexec/` | ACTIVE | Tạo child process theo platform; ẩn console window trên Windows nhưng giữ UAC consent |
| `internal/hostopen/` | ACTIVE | Validate exact regular directory và mở source/backup bằng Explorer hoặc xdg-open; không nhận command text, từ chối symlink/reparse root |
| `internal/multipass/` | ACTIVE | Tìm Multipass, đọc backend driver, xác minh VBoxManage và fail closed nếu VirtualBox không đúng phiên bản pin 7.1.18 |
| `internal/datastore/` | ACTIVE | Đọc/thiết lập Windows `MULTIPASS_STORAGE` trước deploy đầu tiên; guard 0 project/VM, fixed-drive/empty/reparse/free-space validation, hidden UAC helper, rollback và postflight |
| `internal/project/` | ACTIVE | Nhận diện project read-only; auto-discovery immediate child trong conventional web-root, document root và PHP constraint |
| `internal/project/snapshot*.go` | ACTIVE | Copy policy, giới hạn, symlink/reparse guard, deterministic tar.gz, manifest và checksum |
| `internal/state/` | ACTIVE | JSON schema 3, migration schema 1/2, danh sách multi-domain, multi-project/database status/history, validation và atomic persistence theo user profile |
| `internal/vm/` | ACTIVE | Fixed shared Multipass server create/reuse/health-check, recreate guard, verified snapshot upload/immutable guest commit và allowlisted backup download không mount host |
| `internal/runtime/` | ACTIVE | Embedded native package lock, install-once Nginx/PHP/MariaDB, per-site Linux user/PHP systemd sandbox/ACL/schema/origin, resource limits và cross-site health/policy check; cấm Docker |
| `internal/database/` | ACTIVE | Parse WordPress config không execute, local-only dump/file import plan, credential option-file, secure temp/transfer/checksum, guest MariaDB import/verify/cleanup và cập nhật URL trước tunnel |
| `internal/backup/` | ACTIVE | Inspect backup metadata; export đúng per-site source/schema; loại runtime secret/link/special path; download allowlist, SHA-256/tar verification, manifest và cleanup partial backup |
| `internal/sourceeditor/` | ACTIVE | Duyệt/đọc/lưu exact source gốc với containment + symlink/junction guard, UTF-8/extension/2 MB allowlist, secret/dependency denylist, optimistic SHA-256 conflict check, pre-save local backup và atomic temp replace |
| `internal/recovery/` | LEGACY READ-ONLY ENGINE | Module backup/scan revision 37/38 vẫn được giữ để không phá dữ liệu/code cũ nhưng menu revision 39 dùng complete embedded engine bên dưới |
| `internal/recoverytool/` | ACTIVE EMBEDDED ENGINE | Bundle/version extraction, one-time pinned runtime setup, hidden localhost process lifecycle, loopback reverse proxy xác minh server + private identity header độc lập UI, native-vault secret bridge cho recovery và fresh-install, copy-only legacy data plan/import và test tích hợp thật; engine được giữ khi recovery hoặc fresh-install job đang chạy |
| `internal/recoverytool/tool/` | ACTIVE EMBEDDED SOURCE | Toàn bộ code/tests/scripts/docs/theme của recovery engine và `fresh_install.py`/`fresh_ui.py`; fresh installer có create/edit/retry/history, compact Recovery-style UI không lặp page heading, popup chi tiết live-poll tuần tự 1,2 giây + `aria-live`, timestamp/stage/current-file/byte log, FTP-derived DB fallback, read-only capacity probe và exact-root wipe. WordPress chính thức được cache, xác minh, lọc, giải nén/ghép Bricks/child/plugin ở máy OneClick; final files được cân bằng theo byte + số lệnh và upload thẳng qua đúng N kết nối FTP bền, còn HTTPS bridge chỉ xác minh tiny signed manifest/file checkpoint rồi finalize. Recovery FTP streaming giữ nguyên; bundle không chứa external launcher EXE, `.venv`, cache, sites, backup, report, repair, log hoặc credential runtime |
| `internal/recoverytool/tool/assets/fresh-wordpress/` | ACTIVE VERIFIED ASSETS | Bricks 2.3.10, Bricks Child và Duy Anh Web Pro 1.2.3 cùng manifest SHA-256 cố định; được embed vào EXE và chỉ merge sau khi checksum/cấu trúc ZIP/symlink/traversal/size guard đạt |
| `internal/cloudflare/` | ACTIVE | Cloudflare API client, zone/nameserver verification bằng system + public DNS consensus, Named Tunnel config, managed DNS create/update/delete và conflict guard |
| `internal/tunnel/` | ACTIVE | SHA-pinned official cloudflared binary install-once, per-site systemd credential/connector user/service, UID egress firewall, origin binding, start/stop và external HTTPS health-check |
| `internal/secrets/` | ACTIVE | Lưu/đọc/xóa Cloudflare API token, recovery FTP/FTPS password và JSON credential bundle FTP/DB/admin của fresh installer bằng native OS credential vault; backend không trả secret về frontend và không ghi password vào state/profile/history JSON mới |
| `frontend/package.json`, `frontend/package-lock.json` | ACTIVE | React/Vite dependency và scripts |
| `frontend/src/App.tsx` | ACTIVE | Desktop navigation có menu Domain, `Cài mới WP` và Khôi phục WordPress riêng; hai module dùng cùng engine/iframe localhost đã kiểm soát nhưng route/dữ liệu tách biệt; Settings có legacy scan/confirm/progress/result; readiness/install, guarded storage setup, auto-discovery, per-project detail, Source/Database/backup, Source Manager và modal xuất bản |
| `frontend/src/App.css`, `frontend/src/style.css` | ACTIVE | Design tokens CRM, fixed sidebar/content scroll, typography toàn app ≥14 px, danh sách/module Khôi phục full-width, action button 36 px, states, accessibility và reduced motion |
| `frontend/scripts/check-ui.mjs` | ACTIVE TEST | Chặn CSS font dưới 14 px, danh sách không full-width, button sai chiều cao 36 px và bắt buộc focus-visible/reduced-motion trước frontend build |
| `frontend/wailsjs/` | GENERATED | TypeScript bridge/models do Wails sinh từ Go methods |
| `frontend/dist/` | GENERATED/IGNORED | Frontend production bundle được embed vào executable |
| `build/` | ACTIVE + GENERATED OUTPUT | Icon/manifest/installer template và Windows executable output |
| `diagnostics/` | LOCAL DIAGNOSTIC | Hardening log và metadata backup từ clean-repair trên máy test; không được đóng gói vào EXE hoặc chứa source website |

### Cấu trúc dự kiến tiếp theo

Tên file có thể thay đổi lúc triển khai đầu tiên; nếu thay đổi phải sửa bảng này ngay.

| Path dự kiến | Trách nhiệm |
|---|---|
| `internal/platform/windows/` | Windows detection, Hyper-V/Multipass integration |
| `internal/platform/ubuntu/` | Ubuntu/KVM/Multipass integration |
| `internal/vm/` (mở rộng) | Stop/reset/destroy, persisted lifecycle và operation journal |
| `internal/project/` (mở rộng) | Persist/validate config, copy manifest và snapshot policy |
| `internal/network/` | Firewall/egress policy |
| `guest/` | Cloud-init, guest agent, runtime templates |
| `configs/` | Policy/config dùng chung nếu vượt quá embedded runtime version lock hiện tại |
| `tests/` | Integration/e2e fixtures và platform tests |

---

## 15. Current Implementation Snapshot

| Capability | Trạng thái | Bằng chứng/Ghi chú |
|---|---|---|
| Product/security/UI specification | `DONE` | Có trong README revision 63 |
| Wails/Go/React desktop scaffold | `DONE` | Frontend build và Windows executable build thành công |
| Desktop navigation + Setup/Overview/Website/Domain/Fresh WP/Recovery UI | `REV62_FRESH_LIVE_LOG_MODAL_BUILD_PASS` | `Cài mới WP` dùng cùng nhịp layout màn Khôi phục: KPI ngang, list toolbar và card full-width. Heading/mô tả trùng với shell OneClick đã bỏ. Popup chi tiết đang mở được render lại từ polling tuần tự 1,2 giây, tự theo cuối log nếu user chưa cuộn lên và có `aria-live=polite`; không cần đóng/mở popup. Form/font/button/layout revision 55 giữ nguyên |
| Fast fresh WordPress installer | `REV63_LOCAL_EXTRACT_DIRECT_15_FTP_BUILD_PASS_USER_LIVE_PENDING` | OneClick cache WordPress chính thức, bỏ docs/sample/Akismet/Hello/theme mặc định, ghép Bricks/child/plugin vào final paths ngay trên máy. Cây file được cân bằng theo byte + chi phí mỗi lệnh thành đúng `connection.workers` nhóm; mỗi worker giữ một FTP/FTPS connection và upload thẳng phần của mình vào stage. Hosting không nhận/source-extract ZIP WordPress; tiny control ZIP chỉ chứa manifest JSON, bridge chuẩn bị directory, xác minh exact size + SHA-256 từng file và HMAC đủ N group rồi mới finalize database/WordPress. Exact-root consent, DB marker, random HTTPS token, traversal/symlink guard và quyền `0755/0644/0600` giữ nguyên. Cache hợp lệ được dùng lại ở lần sau. |
| Complete WP Clean Rebuild integration | `REV45_ENGINE_START_LIVE_PASS` | Không link runtime tới `D:\DuyAnhWeb`. Delete path/guard revision 43 giữ nguyên. Bundle `9be8f8316e688b11…` đã sync vào runtime thật; server-state ghi cổng localhost sau khi probe mới pass. Close app lúc idle kill engine + cleanup secret; nếu có active job thì engine được giữ có chủ đích để job không bị cắt và app lần sau recover/attach |
| Recovery FTP file download | `REV49_STREAMING_INVENTORY_BUILD_PASS_USER_LIVE_PENDING` | `iter_files_recursive` phát file ngay khi MLSD gặp file; `download_tree` submit vào hàng đợi tối đa 4× worker và tiêu thụ kết quả trong khi inventory còn chạy. Progress phân biệt tổng đang tăng với tổng đã chốt; retry, partial resume, low-concurrency reconcile và manifest/integrity cuối giữ nguyên. Relative/fallback RETR revision 47 và wrapper revision 48 tiếp tục được test |
| Recovery FTP rebuild delete/upload | `REV51_WORKER_TELEMETRY_BUILD_PASS_USER_LIVE_PENDING` | Luồng cuốn chiếu revision 50 giữ nguyên. Profile `phongkhamkbtoancau.com` cấu hình và probe đều xác nhận 16 worker; upload engine dùng đủ 16 vì process-wide slot ceiling là 32. GUI giờ reset pass/worker/ETA/location khi đổi phase và mỗi progress upload phát số worker thật. Rebuild gate nhắc chỉ có thể xóa thủ công trước khi bấm xác nhận, bên trong đúng remote root, giữ `.well-known`, không xóa root và không thao tác song song với app |
| WP Clean Rebuild legacy data import | `REV43_IMPORT_COMPLETE_USER_REQUESTED_LOCAL_CLEANUP_DONE` | Journal giữ bằng chứng import 229.036 file/32.278.821.356 byte/skipped 0. Theo yêu cầu user, hai project imported đã được xóa khỏi workspace; `sites`, `backups`, `repairs` đều trống và không còn report directory theo hai host |
| WordPress recovery source backup/scan legacy | `REV38_CODE_RETAINED_MENU_SUPERSEDED` | Neutral `.ocblob` read-only engine cũ vẫn nằm trong `internal/recovery` và không bị xóa, nhưng menu revision 39 dùng full embedded WP Clean Rebuild engine |
| Readiness Windows | `DONE_MVP` | Platform, virtualization, RAM/disk, Multipass/backend/VirtualBox, network, GUI runtime |
| Readiness Ubuntu | `CODED_NOT_TESTED` | Linux KVM/RAM/disk detection đã code, cần build/test trên Ubuntu |
| Multipass installer Windows | `CODED_USER_TEST_PENDING` | Pin 1.16.3; official GitHub allowlist; Authenticode Canonical; UAC; MSI exit/reboot handling |
| VirtualBox installer Windows | `CLEAN_REPLACEMENT_LIVE_PASS` | Root cause dual 7.2.16+7.1.18 MSI; clean uninstall/install pass; code mới tự gỡ conflict với recoverable stale-dir guard |
| Multipass installer Ubuntu | `CODED_NOT_TESTED` | Snap track `1.16/stable` + `pkexec`; cần Ubuntu test |
| Windows hidden child process | `DONE_MVP_REV53_FRESH_LIFECYCLE_VERIFIED` | `hostexec` no-window; command cài/kiểm tra thông thường tự kết thúc. App shutdown đóng proxy và kill engine khi idle; active recovery hoặc fresh-install job được giữ để tránh cắt dữ liệu, không hiện terminal, và app lần sau nối lại |
| Windows Multipass storage placement | `LIVE_PASS_WINDOWS_HOME` | `MULTIPASS_STORAGE=D:\oneclick-dev-server\data\multipass`; journal helper success, service Running; Ubuntu 24.04.4 boot/SSH pass từ kho mới; VM smoke đã purge |
| Project detection | `LIVE_PASS_WINDOWS` | Unit test immediate-only/no recurse; live nhận 12 WordPress trong `D:\laragon\www`, gồm đúng `flatsome` và `noibo.kientrucnhacuagio` |
| Project configuration + history | `DONE_MVP_SCHEMA3_LIVE` | Backend-validated JSON schema 3 atomic; migration schema 1/2; 4 project đang `public`; backup và source-edit audit chỉ thêm metadata riêng, không đổi stage/runtime/tunnel/last error và không lưu credential, SQL hoặc nội dung file |
| Multipass VM create/reuse | `REV35_SHARED_SERVER_4_SITE_LIVE` | Fixed `oneclick-server`, Ubuntu 24.04, 2 CPU/4 GB/40 GB; runtime/database/tunnel thật đang chạy cho `flatsome`, `flatsome-demo`, `noibo.kientrucnhacuagio`, `freshfads` |
| Multipass failed-create recovery | `SHARED_DATA_GUARD_BUILT` | Recreate chỉ khi toàn state chưa có snapshot/runtime/database/tunnel; có dữ liệu bất kỳ project nào thì từ chối xoá shared server |
| Immutable snapshot + guest transfer | `HISTORICAL_LIVE_PASS_CLEAN_BASELINE` | Pipeline checksum/readonly/current-link đã pass; snapshot cũ đã bị xóa cùng VM, source host giữ nguyên |
| Guest provisioning | `REV33_NATIVE_FULL_LIVE_PASS` | `flatsome` live pipeline đạt 100%: package reuse, site user/config, PHP-FPM active, runtime-dir/socket ACL hiệu lực, Nginx HTTP, MariaDB schema/user/limits và cross-site policy pass; không Docker |
| WordPress database import | `REV35_FULL_LIVE_PASS_BACKUP_GUARD` | Import local-only hiện có giữ nguyên; replacement ở `database_ready` bắt buộc có completed backup, còn public phải dừng domain trước. `flatsome` hiện 12 bảng và backup SQL live 465.792 byte |
| Source/database management + local backup | `REV36_EDITOR_BUILD_PASS_LIVE_BACKUP_INHERITED` | Màn source manager đọc/lưu source mẫu end-to-end qua App bridge; pre-save backup, stale-hash/path/secret/binary/link/dependency guard và audit-state tests pass. Live export `flatsome` revision 35 giữ nguyên: source 35.710.143 byte + SQL 465.792 byte; editor chưa ghi vào 4 source thật trong phiên build |
| Quick Tunnel | `NOT_PLANNED_MVP` | MVP ưu tiên domain ổn định `kidgrow.site` |
| Named Tunnel | `REV34_EXTERNAL_HTTPS_LIVE_PASS` | `flatsome.kidgrow.site` đã tạo tunnel/DNS và qua HTTPS thật; nhiều zone/token vault, binary pin/checksum, per-site LoadCredential/user/systemd/firewall/origin giữ nguyên |
| Private Tunnel/Access | `NOT_STARTED` | — |
| Security firewall policy | `REV31_CODE_BASH_UNIT_PASS_LIVE_PENDING` | PHP không IP network; tunnel UID chỉ đúng origin/DNS/Cloudflare và chặn private/reserved; cross-user/config/DB policy tests fail closed; chờ guest live |
| Automated tests | `REV63_DIRECT_UPLOAD_TARGETED_PASS` | Chỉ chạy test liên quan thay đổi trong `tests/test_fresh_install.py`: `4 passed, 9 deselected` trong 3,55 giây. Xác nhận local final tree/asset filtering, Windows long path, đúng 15 persistent FTP client, live byte/file progress, authenticated manifest/hash contract và PHP 8.4 lint; không chạy Go/full suite/live hosting. |
| Windows standalone executable | `REV63_BUILD_PASS` | Chỉ một `build/bin/oneclick-dev-server.exe`, 42.406.400 byte, SHA-256 `BEF7513F71AD53E5BA0AB185EDFF1CA721478D465CEF09A960605228D0FA9214`. `.venv`, pytest cache và generated Python cache đã dọn trước final build. |
| Installer/release artifacts | `NOT_STARTED` | — |

---

## 16. Roadmap thực thi

### Milestone 1 — Desktop readiness — `DONE_MVP`

- Scaffold Wails v2 + Go + React TypeScript.
- Desktop navigation và giao diện Setup/Overview/Website bằng tiếng Việt.
- Detect Windows/Ubuntu/architecture.
- Kiểm tra virtualization, RAM, disk, Multipass, Cloudflare network và GUI runtime.
- Structured report từ Go sang UI; technical detail được thu gọn.
- Không có mutation.

### Milestone 2 — Dependency readiness — `IN_PROGRESS`

- Setup Wizard sinh installation plan trực quan. `DONE_MVP`
- Nút `Cài đặt/Sửa`, consent, progress và scoped elevation. `DONE_MVP`
- Cài Multipass theo platform. `CODED_USER_TEST_PENDING`
- Detect/cài VirtualBox khi Multipass Windows dùng driver `virtualbox`. `INSTALL_PASS_UX_RETEST`
- Cài/bật các hypervisor dependency còn lại theo platform. `NOT_STARTED`
- Reboot/resume trên Windows.
- Postflight xác minh bằng VM health-check. `CODED_USER_TEST_PENDING`

### Milestone 3 — Isolated local deployment — `IN_PROGRESS`

- Shared Ubuntu server create/reuse + ownership/role/OS/arch/state/no-mount/cloud-init health-check. `REV31_CODED_LIVE_PENDING`
- Recreate shared server chỉ khi chưa có dữ liệu bất kỳ website nào. `REV31_CODED`
- Persist cấu hình, trạng thái và lịch sử nhiều project. `DONE_MVP`
- Stop/reset/destroy và đối chiếu lifecycle VM thực tế. `NOT_STARTED`
- Copy snapshot không mount host. `DONE_MVP_WINDOWS_LIVE`
- Project detection và cấu hình cơ bản. `DONE_MVP`
- Native WordPress runtime adapter install-once + per-site systemd/database isolation. `REV31_CODE_BASH_UNIT_PASS_LIVE_PENDING`
- Generic PHP/static runtime adapter. `NOT_STARTED`
- Runtime/user/systemd/ACL/database hardening. `REV31_CODE_BASH_UNIT_PASS_LIVE_PENDING`
- Local cross-site/policy/HTTP health check. `REV31_CODE_BASH_UNIT_PASS_LIVE_PENDING`
- WordPress database auto-export/manual import, verify và retry độc lập. `REV31_CODE_UNIT_PASS_LIVE_PENDING`
- Per-project source/database inspect, local backup/export và pre-replacement backup guard. `REV35_LIVE_PASS`
- Update working copy từ source gốc với health-check/rollback. `NOT_STARTED`

### Milestone 4 — Stable public domain — `CODED_EXTERNAL_TEST_PENDING`

- SHA-pinned `cloudflared` binary install-once, per-site non-root service/credential/firewall trong VM. `REV31_CODE_BASH_UNIT_PASS_LIVE_PENDING`
- Named Tunnel + managed custom hostname/CNAME. `CODED_CLOUDFLARE_LIVE_PENDING`
- Native API-token vault, connector token-file/secret và rollback/redaction. `CODED`
- Stop xóa public route nhưng giữ runtime/data. `CODED_LIVE_PENDING`
- External HTTPS health check trước khi ghi trạng thái `public`. `CODED_LIVE_PENDING`

### Milestone 5 — Private publication

- Cloudflare Access mode.
- Token lifecycle/revoke.

### Milestone 6 — Compatibility và packaging

- Laravel/generic PHP adapters và database adapter tương ứng.
- Windows/Ubuntu installers.
- Signed update, SBOM, e2e security tests.
- Hoàn thiện installer bootstrap WebView2 và Ubuntu package dependencies.

### Milestone 7 — WordPress clean recovery — `IN_PROGRESS`

- Complete `wp-clean-rebuild` code/tests/scripts/theme nằm trong cùng OneClick EXE; không link thư mục dev và không đóng gói launcher EXE thứ hai. `REV43_DONE`
- Runtime Python/dependency cài một lần theo lock vào application drive; menu sau reuse, child localhost/browser/terminal đều ẩn. `REV43_LIVE_LOCAL_PASS`
- Dashboard local delete có exact-name confirmation, progress/retry, active-workflow guard, legacy path rebase, Windows extended path và bounded retry; hai imported project đã live-delete pass. `REV43_LIVE_PASS`
- Profile mới không trả password về frontend và không persist password trong JSON; native vault là source of truth. Xóa local dọn cả runtime secret và native-vault credential; idle shutdown vẫn dọn secret directory/server state. `REV43_TEST_PASS`
- Settings legacy import copy-only, scan/confirm/progress/result; không xóa source, không overwrite conflict. `REV39_REAL_229036_FILE_SCAN_PASS_COPY_USER_CONFIRM_PENDING`
- Database acquisition, scan, clean build và rebuild workflow hiện có của engine đã đi cùng bundle; destructive hosting execution vẫn cần user thao tác/confirmation và live evidence riêng. `REV39_ENGINE_AVAILABLE_USER_HOSTING_TEST_PENDING`
- Theo yêu cầu vận hành revision 55, Cài mới WP dùng một mật khẩu chung cho FTP, database và WordPress admin để rút gọn form; mật khẩu vẫn chỉ nằm trong native vault/runtime secret và HTTPS bridge body, không vào JSON/log/argv. Đây là trade-off tiện dụng có blast radius lớn hơn credential tách riêng. `USER_ACCEPTED_SECURITY_TRADEOFF`

---

## 17. Test Matrix và Definition of Done

### Platform matrix

| Nền tảng | Desktop/Readiness | Install | VM | Deploy | Tunnel | Destroy |
|---|---|---|---|---|---|---|
| Windows 10/11 Pro x64 + Hyper-V | BUILD_PASS / USER_TEST_PENDING | CODED / USER_TEST_PENDING | CODED / RETEST_PENDING | NOT_RUN | NOT_RUN | NOT_RUN |
| Windows Home x64 + VirtualBox | REV55 FRESH COMPACT-UI/FILE-LOG/DB-FALLBACK BUILD PASS / HOSTING LIVE PENDING; REV52 RECOVERY RUNTIME COPY BUILD PASS; REV51 FTP WORKER TELEMETRY BUILD PASS; REV50 FTP REBUILD STREAMING BUILD PASS; REV49 BACKUP STREAMING BUILD PASS; REV46 RECOVERY WEBSITE-LAYOUT BUILD PASS; REV45 ENGINE LIVE PASS; STORAGE LIVE PASS | CLEAN_7.1.18_LIVE_PASS | SHARED SERVER 4-SITE LIVE | NATIVE RUNTIME + DATABASE + BACKUP LIVE PASS | 4 CUSTOM-DOMAIN HTTPS 200 (revision 35 live evidence) | SHARED PER-SITE DESTROY NOT STARTED |
| Ubuntu 22.04 x64 + KVM | NOT_RUN | NOT_RUN | NOT_RUN | NOT_RUN | NOT_RUN | NOT_RUN |
| Ubuntu 24.04 x64 + KVM | NOT_RUN | NOT_RUN | NOT_RUN | NOT_RUN | NOT_RUN | NOT_RUN |

### Security acceptance tests bắt buộc

- Webshell trong app không đọc được một canary file trên host.
- Webshell không ghi được vào source project trên host.
- Guest không cài/chạy Docker/Compose/containerd; website không có daemon/control socket.
- Site user A không đọc được `wp-config.php`, config/private/source riêng của site B; Nginx chỉ có ACL read/traverse cần thiết và không đọc wp-config.
- PHP-FPM site không tạo được AF_INET/AF_INET6 socket, không capability/device/namespace và không ghi ngoài site/private directory.
- DB user chỉ có đúng privilege allowlist trên schema của mình, không global/cross-schema; giới hạn 8 connection và 30 giây/statement phải được verify.
- Tunnel user không đọc được site secret; token không xuất hiện trong argv/environment/state/log; firewall UID chỉ cho đúng origin, local DNS và Cloudflare TCP 443/7844.
- VM/app không kết nối được tới host gateway và private LAN mặc định.
- CPU/memory/PID bomb bị giới hạn, host vẫn sử dụng được.
- App không public nếu firewall setup thất bại.
- `logs` và crash report không chứa tunnel token/DB password.
- `destroy` dừng URL public và chỉ xoá resource đúng site; không xoá shared VM hoặc dữ liệu website khác.
- Dependency installer không chạy nếu chưa có consent.
- Storage bootstrap không chạy nếu chưa xác nhận riêng, đã có project/VM, target không phải fixed local drive/trống, target là reparse point hoặc còn dưới 10 GB; frontend không được truyền command/script.
- Storage bootstrap lỗi phải bỏ `MULTIPASS_STORAGE` mới, khởi động lại dịch vụ mặc định và không xóa source/project/state/token; không được chuyển nóng deployment.
- Artifact sai checksum/signature bị từ chối.
- Backend Multipass dùng `virtualbox` nhưng thiếu/không chạy được `VBoxManage.exe` phải chặn tạo VM trước mutation.
- `state.json` phải ghi atomic, từ chối schema/unknown fields/file quá lớn và không chứa secret hoặc nội dung website.
- `recovery.json` phải nằm dưới application drive, ghi atomic, schema/size/project/history có giới hạn và không chứa FTP/FTPS password. Password chỉ ở native OS credential vault; backend không trả password về frontend/list response và không đưa vào argument/environment/log.
- Kiểm tra kết nối khôi phục chỉ được mở control channel, xác minh TLS certificate với TLS 1.2+, đăng nhập và `CWD` exact remote path; không list/download/upload/delete/execute. Plain FTP chỉ được lưu sau xác nhận rủi ro tường minh.
- Sao lưu khôi phục chỉ được chạy sau connection-ready và confirmation plan. Recursive listing chỉ nhận MLSD file/dir hợp lệ, passive data connection luôn dùng host đã xác nhận thay vì IP PASV do server cung cấp, chặn control/path traversal/symlink/unknown type, áp 100.000 file/20 GB/2 GB/depth/time limits. Remote bytes phải được lưu bằng SHA-256 `.ocblob` trung tính, reverify trước atomic commit; partial bị dọn khi lỗi. Không giữ đuôi executable, execute/extract, upload, rename hoặc delete remote data; password không vào manifest/report/JSON/log.
- FTP backup phải `CWD` vào exact remote tree đã được containment-check rồi gửi `RETR` bằng đường dẫn tương đối; chỉ fallback một lần sang absolute path khi daemon từ chối relative. Inventory phải phát file theo iterator và RETR bằng hàng đợi giới hạn ngay trong lúc duyệt; progress phải đánh dấu tổng đang tăng cho tới khi inventory hoàn tất, không được giả đây là tổng cuối. Reconnect phải đặt lại CWD trước resume; phản hồi lỗi phải được redaction rồi lưu vào progress/log, không được biến hàng nghìn lỗi thành `0/N` không có nguyên nhân. Sau streaming, danh sách đầy đủ vẫn phải qua retry/reconcile, cleanup partial và integrity/manifest như cũ.
- Auto database không được chạy `wp-config.php`, truy cập remote/LAN DB hoặc đưa password vào argument/environment/frontend/state/log; credential file/dump tạm phải nằm ở application data drive, mode riêng và được dọn.
- Auto database phải probe loopback trước dump; chỉ tự start server/config regular-file nằm trong đúng cây Laragon/XAMPP đã nhận diện, không nhận executable/argument từ frontend/source, không mở terminal và phải ép server do OneClick start chỉ bind loopback.
- SQL import phải verify regular-file/extension/size/SHA-256/gzip/table prefix, chỉ drop/recreate schema `oc_<project-id>`, revoke/regrant privilege allowlist rồi import bằng site DB user, xác minh bảng WordPress và không bind DB port ra host/LAN.
- Backup download chỉ được nhận exact per-project export path/file allowlist; phải loại `wp-config.php`/`.env*`, symlink/special/traversal, kiểm tra size/SHA-256 và xóa partial directory khi lỗi. Database replacement bắt buộc completed backup và tunnel đã dừng.
- Source editor chỉ được dùng exact project path đã lưu trong backend; từ chối absolute/traversal, symlink/junction/reparse, non-regular/binary/non-UTF-8, file quá 2 MB, dependency tree và file secret/key. Save bắt buộc expected SHA-256 còn khớp, tạo backup local trước, ghi qua temp + sync + replace và không lưu nội dung vào JSON history. Edit source gốc không được tự cập nhật working copy/public website.
- Document root tuyệt đối hoặc thoát ra ngoài project bằng `..` bị từ chối ở backend trước snapshot.
- Create VM chỉ dùng fixed name `oneclick-server` và image/resource cố định; không nhận command/image/resource tuỳ ý từ frontend.
- VM được tạo không có host mount/bridge; health-check phải từ chối instance không có OneClick marker, sai Ubuntu/arch/state hoặc có mount.
- Recovery chỉ được purge `oneclick-server` sau xác nhận riêng khi toàn state không có snapshot/runtime/database/tunnel. Có dữ liệu của bất kỳ project nào phải fail closed; VM còn IP/SSH cũng không được purge.

### UI acceptance tests bắt buộc

- Nhân viên có thể đi từ mở ứng dụng tới chọn website mà không dùng terminal.
- Sidebar phải giữ nguyên vị trí theo viewport; chỉ vùng content bên phải cuộn dọc, không xuất hiện horizontal scroll hoặc che CTA cuối trang.
- Màn Website chỉ hiển thị danh sách đã lưu và auto-discovery; không đặt tiến trình deploy sau mục `Đã tìm thấy trên máy`. Nút `Chi tiết` phải mở màn riêng của đúng website, chứa tiến trình, hành động tiếp theo và lịch sử; quay lại phải trở về danh sách dự đoán được.
- Màn Cài đặt phải hiện current/recommended storage path, free space, project/VM count và trạng thái khóa; chọn thư mục chỉ chuẩn bị plan, UAC chỉ xuất hiện sau nút xác nhận thứ hai.
- Domain phải là menu cấp cao riêng trong sidebar; Cài đặt không lặp lại danh sách domain. Trang Domain phải có danh sách/trạng thái, thêm/cập nhật token, xác nhận xóa và điều hướng được trực tiếp từ CTA Website.
- `Khôi phục WordPress` phải là menu cấp cao riêng và tải complete embedded dashboard trong content area; không mở browser, terminal hoặc EXE thứ hai. Engine/proxy chỉ bind random `127.0.0.1`, proxy phải xác minh server marker trước khi nhúng, có loading/setup/error/retry và iframe title/sandbox/no-referrer.
- Cài đặt phải có `Nhập dữ liệu WP Clean Rebuild`: chọn đúng workspace cũ, quét file/byte/profile trước, hiển thị destination, yêu cầu xác nhận riêng rồi mới copy, phát progress file/byte và không tự xóa nguồn. Link/reparse/special/traversal/over-limit/conflict phải fail closed; retry bỏ qua file đã xác minh trùng.
- Password FTP/FTPS của embedded engine không được trả trong API payload hoặc ghi profile JSON mới. Import phải chuyển password cũ sang native vault; secret file runtime phải nằm trong application data, mode riêng, không qua argv/log, và bị xóa khi OneClick đóng lúc không có job đang chạy.
- `Cài mới WP` phải lưu profile/history riêng dưới `fresh-installs`, không ghi FTP/DB/admin password vào JSON/log/argv. Domain và URL phải khớp, URL bắt buộc HTTPS. Nút đo FTP chỉ được login/CWD/NOOP rồi đóng session, tối đa 16 connection và không được ghi secret/mutate hosting. Remote path `/` bị từ chối; nội dung cũ chỉ được xóa sau checkbox xác nhận đã persist, trong exact website root, không xóa root và giữ `.well-known`; sau wipe phải list lại và fail closed nếu còn mục chặn. Database chỉ được trống hoặc có checkpoint đúng install ID. Asset checksum, ZIP traversal/symlink/duplicate/size guard, temporary randomized bridge + token header, remote archive hash, server-side cleanup và kích hoạt exact Bricks Child/plugin đều phải pass trước trạng thái thành công.
- Form `Cài mới WP` phải tự đồng bộ domain → tên website/HTTPS URL/FTP host/DirectAdmin path/admin email; FTP username → `<user>_db`/`<user>_user`, còn một mật khẩu FTP được dùng chung cho database và WordPress admin theo quyết định revision 55. Tên database/user vẫn sửa được; host/path/port/protocol/table prefix nằm trong `Cấu hình nâng cao`. Probe luồng phải có loading/disabled/result rõ; destructive wipe phải có nhãn cảnh báo và checkbox bắt buộc, không ẩn trong primary CTA.
- Chi tiết `Cài mới WP` phải hiển thị log tối thiểu gồm thời gian, stage, phần trăm, file/path hiện tại, byte đã tải/tổng byte, thời gian chạy và tín hiệu cuối. Trong lúc PHP bridge xử lý lâu, heartbeat 5 giây phải cập nhật để UI không bị hiểu nhầm là treo.
- Bộ cài mới phải loại Akismet, Hello Dolly và toàn bộ theme Twenty*; chỉ Bricks Child được kích hoạt. Archive lớn phải chia tối đa theo worker FTP đã đo (trần 16), mỗi phần dùng connection riêng; PHP bridge chỉ được ghép phần có tên ngẫu nhiên đã pin, xác minh SHA-256 archive hoàn chỉnh rồi mới giải nén và xóa file tạm.
- Fresh install phải xử lý database trước mọi download lớn. Sau khi candidate đã lưu thất bại, bridge chỉ được dùng credential hosting nhận qua HTTPS body để gọi DirectAdmin trên loopback `127.0.0.1:2222`; không gửi credential panel qua HTTP mạng ngoài, không dò mật khẩu/cổng. Core chỉ tải từ HTTPS chính thức của WordPress với TLS verification; ZIP core/resource đều phải qua traversal, symlink, count và unpacked-size guard.
- Khi xuất bản website, modal chỉ liệt kê zone đang `Sẵn sàng`, hiển thị hostname hoàn chỉnh và lưu zone đã chọn cho stop/recovery của đúng project.
- Deploy CTA bị vô hiệu khi required readiness check chưa `READY`.
- Loading, disabled, success, warning và error đều có text/icon, không phụ thuộc chỉ vào màu.
- Mọi control dùng được bằng keyboard và có focus ring rõ ràng.
- Technical detail mặc định thu gọn; lỗi chính có nút khắc phục ngay cạnh.
- Không có emoji làm navigation/status icon; mọi text hiển thị tối thiểu 14 px; mọi action button chính/phụ/tertiary và icon button cao 36 px, không biến thể tự giảm chiều cao, và có focus ring.
- Danh sách website, domain và Khôi phục phải dùng toàn bộ chiều rộng cột content; giao diện Khôi phục không hiển thị tên công cụ legacy.
- Dashboard Khôi phục phải theo cùng bố cục màn Website: KPI ngang ở đầu, tiêu đề danh sách kèm action, mỗi dự án là một card full-width; không dùng table header hoặc khung dashboard lồng dư thừa.
- `prefers-reduced-motion` làm animation gần như tức thời.
- Kiểm tra/cài dependency trên Windows không làm terminal hoặc PowerShell nhấp nháy; chỉ UAC có chủ đích được hiện.
- Cài đặt dài phải cập nhật progress định kỳ và giải thích thời gian chờ; không giữ nguyên nhãn `Hãy xác nhận` sau khi UAC đã được chấp thuận.
- Runtime đầu tiên phải nói rõ đang cài package dùng chung; website sau reuse package/Nginx/MariaDB/cloudflared và không cài lại Ubuntu/runtime.
- Guest phase phải có timeout riêng; service restart không được giữ Multipass exec channel và phase idempotent không restart daemon nếu config/service không đổi.
- Chọn website xong tự mở bước cấu hình; mọi nút hành động phải đổi trạng thái hoặc điều hướng, không có nút rỗng.
- `Tạo môi trường` phải có màn xác nhận tài nguyên/cách ly, disabled state, tiến trình dài, kết quả lỗi có retry và thành công có VM/IP.
- Mở lại app phải thấy tổng số project, trạng thái/bước cuối và lịch sử; project lỗi/dang dở phải có hành động tiếp tục hoặc thử lại rõ ràng.
- Màn Website phải tự liệt kê immediate project hợp lệ trong Laragon/XAMPP/WampServer, không recurse/toàn ổ và không ghi project vào JSON trước khi người dùng bấm `Thiết lập`.
- VM giữ `Starting` phải có progress/timeout; CTA `Tạo lại từ đầu` chỉ xuất hiện khi shared server chưa có dữ liệu website.
- VirtualBox khác exact 7.1.18 phải bị readiness chặn trước create và đưa CTA cài phiên bản tương thích; app không tự sửa firmware VM.
- VirtualBox CLI đúng 7.1.18 nhưng driver `.sys` khác version hoặc còn `PendingFileRenameOperations` phải trả yêu cầu restart và không được gọi `multipass launch`.
- Existing VM `Running` với IP `N/A` nhưng SSH hoạt động phải tiếp tục thành công; chỉ khi SSH timeout mới trả CTA tạo lại trong khoảng 12 giây.
- Snapshot phải bỏ secret/cache/log/DB dump, từ chối symlink/reparse và traversal, áp giới hạn file/bytes, verify SHA-256 ở guest, không có host mount và không chạy source.
- Source không đổi phải sinh cùng snapshot ID; retry dùng lại immutable guest snapshot, không tạo thêm deployment directory.
- Runtime plan phải nói rõ database trắng, lượng tải, giới hạn tài nguyên và `chưa public`; tác vụ dài phải pulse progress, retry được sau restart app.
- Runtime phải cấm Docker, pin package revision, dùng per-site user/PHP systemd/Unix socket/schema/origin, no-new-privileges/capability rỗng/network deny/resource cap và cross-site policy/HTTP pass trước `runtime_ready`.
- Sau `runtime_ready`, UI phải có bước database riêng với `Tự lấy từ WordPress` và `Chọn file`; hiển thị nguồn/tiền tố/dung lượng, cảnh báo thay database đích, progress, số bảng, retry chỉ database và không cho chọn domain trước `database_ready`.
- Chi tiết website phải có khối Source/Database riêng: mở exact source gốc, hiển thị schema/table/backup, progress/result rõ, mở backup folder; website public không được thay database trực tiếp và chưa có backup không được bật action replacement.
- `Quản lý source` phải mở màn riêng trong app với breadcrumb/file list/editor, nêu rõ đang sửa source gốc và website public chưa đổi; file bị khóa có lý do, unsaved không bị bỏ qua khi chuyển file/quay lại, save/reload/success/error phải có feedback text và Explorer chỉ là action phụ.
- App đóng ở `database_importing` phải phục hồi `database_failed`; schema 2 cũ phải migrate schema 3. Trước tunnel phải cập nhật `home/siteurl` theo hostname đã chọn hoặc rollback và fail closed.
- Domain phải thuộc zone đã kết nối, nameserver Cloudflare phải active; DNS ngoài project không được ghi đè. API token không được backend trả lại hoặc ghi state/log; connector firewall phải có trước service start và HTTPS thật phải pass trước stage `public`.

Một milestone chỉ `DONE` khi code, unit/integration test, platform evidence, File Map, Snapshot và Change Log đều đã cập nhật.

---

## 18. Decision Log

| ID | Quyết định | Lý do | Trạng thái |
|---|---|---|---|
| ADR-001 | Dùng VM làm security boundary, không dùng container trực tiếp trên host | Container trên Linux chia sẻ kernel; yêu cầu giới hạn ảnh hưởng khi website bị compromise | ACCEPTED |
| ADR-002 | Dùng Multipass ở MVP | API/CLI đồng nhất tương đối giữa Windows và Ubuntu; giảm lượng code quản lý hypervisor | ACCEPTED |
| ADR-003 | `cloudflared` chạy trong guest VM | Không đặt tunnel credential/process cạnh dữ liệu host; tunnel chết cùng sandbox | ACCEPTED |
| ADR-004 | Copy snapshot, không mount project | Ngăn mã độc ghi ngược vào workspace host | ACCEPTED |
| ADR-005 | Go CLI trước desktop UI | Yêu cầu mới xác nhận nhân viên không sử dụng command | SUPERSEDED_BY_ADR-008 |
| ADR-006 | Installer chạy preflight và cần consent | Hệ thống phải sẵn sàng trước deploy nhưng không được tự ý thay đổi máy | ACCEPTED |
| ADR-007 | Windows Home chưa cam kết trong MVP | Cần xác nhận Multipass/VirtualBox và test security/lifecycle thực tế | ACCEPTED |
| ADR-008 | Desktop GUI là sản phẩm chính; CLI không xuất hiện trong luồng nhân viên | Người dùng mục tiêu không có kiến thức command line | ACCEPTED |
| ADR-009 | Wails v2.13 + Go + React TypeScript | Giữ Go cho system logic, dùng WebView native để có UI hiện đại và binary nhẹ; chọn v2 stable thay vì v3 mới | ACCEPTED |
| ADR-010 | Dùng template CRM truyền thống compact cho desktop | Nhân viên cần quét trạng thái nhanh; giảm hero, gradient và mô tả dài gây cảm giác “AI-generated” | ACCEPTED |
| ADR-011 | Child process Windows chạy hidden/no-window | Desktop app không được làm terminal nhấp nháy; UAC vẫn tách riêng để giữ explicit consent | ACCEPTED |
| ADR-012 | VM name do backend sinh từ canonical project path; fixed launch args và cloud-init ownership marker | Frontend không thể điều khiển instance/image/resource; không chiếm dụng hoặc tự xoá VM trùng tên ngoài OneClick | SUPERSEDED_BY_ADR-031 |
| ADR-013 | Lưu lịch sử local bằng JSON schema versioned ngoài source project và ghi atomic | Cần khôi phục nhiều project/bước cuối nhưng không được sửa source hay giữ secret; format nhỏ, đọc được và dễ migrate | ACCEPTED |
| ADR-014 | Backend Multipass `virtualbox` bắt buộc có VBoxManage; VirtualBox installer phải pin nguồn/hash/publisher | Tránh lỗi launch UUID mơ hồ và không chạy mutation khi hypervisor thực tế chưa sẵn sàng | ACCEPTED |
| ADR-015 | VM lỗi chỉ được recreate khi name deterministic khớp state project, không `Running`, và user xác nhận destructive lần hai | Chính sách ban đầu chưa xử lý trạng thái giả `Running + N/A` | SUPERSEDED_BY_ADR-017 |
| ADR-016 | Tách image-download khỏi network/SSH readiness; VirtualBox install luôn cần restart, fresh VM không IP quá 2 phút trả reboot-required | Live clean boot cần 2 phút 25 giây nên timeout 2 phút tạo false failure | SUPERSEDED_BY_ADR_021 |
| ADR-017 | Cho phép confirmed recreate VM `Running` chỉ khi exact recorded deterministic name, không IP usable và SSH probe độc lập thất bại | Multipass có thể báo Running dù guest không boot/SSH; cấm tuyệt đối theo state khiến người dùng không thể phục hồi, còn guard kép ngăn xoá VM đang hoạt động | ACCEPTED |
| ADR-018 | Fresh Windows VirtualBox VM không SSH được tự chuyển EFI→BIOS qua hidden UAC; SSH là connectivity authority khi CLI báo `N/A` | BIOS chỉ boot thành công một lần rồi kẹt initramfs sau restart, nên firmware mutation không đủ ổn định | SUPERSEDED_BY_ADR_019 |
| ADR-019 | Pin exact VirtualBox 7.1.18 và chặn mọi phiên bản khác trước create; không tự sửa firmware | Multipass 1.16.3 phát hành sau nhánh 7.1.18; 7.2.16 trên máy test lặp EFI #GP và BIOS không ổn định. Dependency pin có thể test/reproduce và an toàn hơn mutation VM | ACCEPTED |
| ADR-020 | Readiness kiểm tra cả version kernel driver và pending replacement; mixed-version trả `RebootRequired` trước mutation | Sau downgrade, `VBoxManage` đã là 7.1.18 nhưng `VBoxSup/VBoxUSBMon` vẫn 7.2.16 tới khi Windows restart, làm VirtualBox Hardening dừng VM ngay khi startup | ACCEPTED |
| ADR-021 | Windows VirtualBox phải có đúng một MSI registration; installer clean-replace conflict; fresh SSH timeout là 5 phút | Dual MSI 7.2.16+7.1.18 gây `VERR_VM_DRIVER_VERSION_MISMATCH (-1912)` dù file metadata trông đúng. Fresh clean boot/cloud-init thực tế mất 2 phút 25 giây | ACCEPTED |
| ADR-022 | Snapshot deterministic, secret-deny mặc định, regular-file-only, archive SHA-256 verify trong guest và immutable commit; retry reuse theo checksum | Cần copy một chiều không mount, chống path/link escape, không mang host secret sang guest và tránh mỗi retry tạo bản sao trùng | ACCEPTED |
| ADR-023 | WordPress runtime dùng exact Ubuntu package + OCI digest lock, secret sinh trong guest, working copy riêng và hai Docker internal network không published port | Runtime có thể thực thi mã độc nên phải fail closed, không ghi ngược snapshot/host, không lộ secret và không có ingress/egress trước bước tunnel | SUPERSEDED_BY_ADR-031 |
| ADR-024 | MVP public bằng Cloudflare Named Tunnel cho từng project; zone phải chuyển nameserver Cloudflare; API token ở native vault và connector chỉ ở guest | User cần subdomain ổn định; full setup cho phép quản lý HTTPS/DNS tự động mà không mở port/ẩn origin, đồng thời giữ credential và process tunnel khỏi website/host UI | ACCEPTED |
| ADR-025 | Không chuyển nóng kho dữ liệu Multipass trong app | Saved-state và metadata hypervisor có thể hỏng/không parse nhất quán; source/state/token đã tách riêng, còn vị trí disk VM phải được chọn ở install-time trong một thiết kế sau | ACCEPTED |
| ADR-026 | Auto-discovery chỉ đọc immediate child trong conventional web-root và không tự persist | Nhân viên cần thấy project ngay nhưng quét sâu/toàn ổ vừa chậm vừa vượt phạm vi; xác nhận thủ công trước khi tạo state/VM giữ explicit consent | ACCEPTED |
| ADR-027 | Chỉ cấu hình `MULTIPASS_STORAGE` khi 0 project và 0 VM, dùng thư mục trống trên fixed drive rồi khóa sau lần đầu | Giải quyết tăng dung lượng ổ C mà không lặp lại migration saved-state lỗi; baseline copy giữ authentication, postflight/rollback làm mutation fail closed | ACCEPTED |
| ADR-028 | Đặt nested `data/go.mod` tại ranh giới thư mục dữ liệu ứng dụng | Kho `data/multipass` dùng ACL hệ thống nên Go/Wails không được recurse khi list/build; nested module giữ dữ liệu ở cùng thư mục ổ D nhưng tách khỏi source graph, không đổi runtime storage | ACCEPTED |
| ADR-029 | Lưu nhiều Cloudflare zone trong JSON schema 2, chọn domain tường minh theo website và đặt Domain thành menu cấp cao riêng | Nhân sự phải quản lý domain độc lập với cấu hình hệ thống; mỗi website có thể dùng domain khác nhưng stop/recovery vẫn phải chọn đúng credential và không suy đoán mơ hồ | ACCEPTED |
| ADR-030 | Tách database thành stage sau runtime; auto export chỉ từ WordPress/MySQL local hoặc file do user chọn, credential không qua frontend/argv và import bằng app user trong đúng VM | Source không đủ để deploy WordPress; tách stage cho phép retry nhanh mà không cài lại runtime, còn local-only/one-way/scoped privilege giữ website độc lập và không mở thêm trust vào host/LAN | ACCEPTED |
| ADR-031 | Một shared Ubuntu server, không Docker; package/Nginx/MariaDB/cloudflared binary cài một lần, còn mỗi site tách bằng user/PHP systemd/ACL/schema/origin/tunnel UID firewall | Giảm thời gian và dung lượng khi có nhiều website theo yêu cầu người dùng. VM vẫn bảo vệ host; defense-in-depth chặn compromise ứng dụng thông thường lan ngang. Chấp nhận residual risk shared guest kernel/Nginx/MariaDB/disk và không tuyên bố isolation tuyệt đối như VM-per-site | ACCEPTED_WITH_RECORDED_RESIDUAL_RISK |
| ADR-032 | Cho phép reverse export rất hẹp chỉ cho backup per-site đã xác minh; không sync working copy về source gốc và không host mount | Người dùng cần xem/chỉnh dữ liệu đã deploy. Allowlisted guest path + secret/link/type/tar/checksum guard + app-local destination giữ ranh giới host, trong khi direct working-copy mount/sync có thể cho mã độc ghi ngược vào source | ACCEPTED |
| ADR-033 | Cho phép edit source gốc chỉ sau thao tác Save tường minh trong app; chặn secret/link/binary/dependency, bắt buộc pre-save backup và optimistic SHA-256; không tự sync sang working copy/public website | Người dùng cần giao diện chỉnh source nhưng silent overwrite hoặc sync hai chiều làm tăng đường ghi ngược và có thể phá deployment đang chạy. Edit host có consent + backup/conflict guard giải quyết nhu cầu mà vẫn giữ snapshot/public độc lập | ACCEPTED |
| ADR-034 | Tích hợp `wp-clean-rebuild` thành module UI/engine trong OneClick nhưng thay giao diện browser/launcher và secret model cũ; recovery state tách riêng, password native vault, FTPS mặc định và mutation khóa theo từng phase | Một ứng dụng cho nhân viên là hợp lý nhưng không được mang plaintext credential, password reuse hoặc remote destructive workflow trực tiếp vào deployment control plane. Revision 37 chỉ mở read-only connection preflight; backup/scan/rebuild phải thêm isolation và consent trước khi bật | ACCEPTED_PHASED |
| ADR-035 | Source recovery backup ở revision 38 lưu untrusted bytes bằng content-addressed `.ocblob` trung tính trên application drive, không giữ original extension và không execute/extract; manifest + scanner dùng bounded byte readers và hosting chỉ bị đọc | Cần có backup/scan thật nhưng source có thể chứa webshell. Neutral blob + SHA-256 reverify + partial atomicity làm giảm accidental execution/path escape và giữ bằng chứng; chưa tuyên bố file sạch hoặc mở mutation/rebuild. Database và clean rebuild vẫn cần thiết kế isolation/consent tiếp theo | ACCEPTED_PHASED |
| ADR-036 | Đóng gói toàn bộ source WP Clean Rebuild vào versioned Go embed, chạy engine bằng hidden loopback process qua validating reverse proxy; dữ liệu cũ chỉ nhập copy-only sau plan/confirm và password chuyển native vault | Một app duy nhất phải chứa toàn bộ chức năng nhưng không được phụ thuộc `D:\DuyAnhWeb`, mở browser/terminal/EXE con hoặc sao chép 32 GB ngoài ý muốn. Full engine cần giữ original backup/repair filename để tương thích nên các file nhập được ghi rõ là untrusted, thay vì tuyên bố neutral blob như engine legacy | ACCEPTED_WITH_UNTRUSTED_DATA_BOUNDARY |
| ADR-037 | Cài WordPress mới trên hosting bằng một ZIP đã xác minh và PHP bridge ngắn hạn qua HTTPS; remote root/database phải trống, credential ở native vault và mỗi site có profile/history riêng | Upload hàng nghìn file qua FTP quá chậm. Một archive cache dùng lại loại round-trip per-file; randomized 256-bit-token bridge giải nén/cài/kích hoạt server-side rồi tự xóa. Fail-closed root guard và database checkpoint tránh ghi đè site/DB cũ, còn HTTPS và native vault giới hạn lộ FTP/DB/admin secret | ACCEPTED_WITH_HOSTING_TRUST_BOUNDARY |
| ADR-038 | Cài mới được dọn nội dung cũ trong exact non-root FTP path sau xác nhận riêng; connection-capacity probe hoàn toàn read-only | Hosting thường tự tạo `cgi-bin/index.html`, khiến yêu cầu root trống chặn cài mới. Persist checkbox + reject `/` + containment/reconnect wipe + postflight list cho phép tự động hóa mà không xóa account root; giữ `.well-known` tránh phá HTTPS. Probe login/CWD/NOOP giúp dùng số luồng tối đa đã xác minh nhưng không chuyển/xóa file hoặc persist password mới | ACCEPTED_WITH_EXPLICIT_DESTRUCTIVE_CONSENT |
| ADR-039 | Cài mới WP dùng chung mật khẩu FTP cho database và WordPress admin; database name/user mặc định theo prefix FTP và bridge được phép thử fallback read-only connection | User ưu tiên form nhân viên ngắn và một credential. Secret vẫn không vào JSON/log/argv, nhưng credential reuse làm tăng blast radius nếu một dịch vụ lộ mật khẩu; trade-off này được ghi rõ. Fallback chỉ thử kết nối các tên đã xác định, không tạo/xóa database, rồi lưu lại name/user đã hoạt động | ACCEPTED_USER_REQUEST_WITH_RECORDED_RISK |
| ADR-040 | Fresh WordPress dùng package slim và upload archive theo nhiều phần song song | Site luôn kích hoạt Bricks Child nên starter plugin/theme mặc định chỉ tăng byte và số file; loại chúng khỏi package cache. Một FTP stream không thể tận dụng phép đo 15 connection, vì vậy archive được chia tối đa 16 phần, upload độc lập, ghép server-side và kiểm tra SHA-256 trước cài. Đổi lại WordPress không có default-theme fallback nếu Bricks lỗi; lỗi theme phải được sửa hoặc cài theme khác qua quản trị | ACCEPTED |
| ADR-041 | Hosting tự tải WordPress và bridge tự tạo database qua control-panel loopback | Static analysis EXE tham khảo do user cung cấp cho thấy lợi thế chính là server-side core fetch và control-panel DB provisioning, không phải chỉ tăng FTP threads. OneClick triển khai độc lập: upload resource-only, fixed official WordPress HTTPS URL, DirectAdmin legacy API chỉ trên loopback, rồi verify MySQL. Cách này giảm core transfer qua máy nhưng phụ thuộc hosting có outbound HTTPS/cURL và FTP credential cũng là DirectAdmin main-account credential | ACCEPTED |
| ADR-042 | Dọn source cũ server-side, package chỉ giữ runtime tiếng Việt và xóa site mặc định là xóa hồ sơ local | FTP không có recursive delete portable nên xóa từng file làm thời gian tỷ lệ với số file. Bridge đã xác thực token có thể xóa đúng document root nhanh hơn rồi verify allowlist; resource bỏ `.po`, non-vi `.mo` và test fixture giảm 21 MB xuống 8,17 MB nhưng site muốn ngôn ngữ khác phải bổ sung language pack. Xóa khỏi danh sách không được ngầm xóa source/database thật; destructive remote delete cần một consent riêng nếu thêm sau | ACCEPTED |
| ADR-043 | ZIP WordPress/resource được validate metadata rồi giải nén nguyên khối bằng native libzip | ZIP core đến fixed `https://wordpress.org/latest.zip` với TLS verification; vòng lặp PHP `getStream`/copy từng file khiến `wp-includes` gần 90 MB trở thành nút thắt đơn luồng. Metadata scan không inflate data nên chi phí nhỏ; sau khi pass root/traversal/symlink/count/size, `ZipArchive::extractTo()` chạy native, rồi dọn thành phần mặc định và chuẩn hóa quyền. Không yêu cầu login/control-panel hoặc command shell | ACCEPTED |
| ADR-044 | Số worker cài mới là cấu hình bắt buộc end-to-end cho cả upload và giải nén | Live revision 59 chứng minh upload 8,16 MB xong trong 2 giây nhưng một PHP worker mất 189 giây ở server install. Theo yêu cầu user, OneClick không tự giảm worker theo kích thước ZIP hoặc dự đoán giới hạn PHP: cấu hình 15 tạo 15 FTP part và 15 HTTPS extraction request. Shard file rời nhau, directory precreate, HMAC checkpoint, core/resource phase tách và finalize gate giảm race; hosting từ chối concurrency sẽ trả lỗi worker thay vì âm thầm hạ worker | ACCEPTED_USER_MAX_THROUGHPUT |
| ADR-045 | Mỗi worker nhận một ZIP hợp lệ, cân bằng và chứa thẳng final paths; không split/merge hoặc core/resource trung gian | Chia một ZIP theo byte chỉ giúp FTP nhưng buộc hosting ghi thêm một lần để ghép, rồi tải/extract core và promote `stage/wordpress`, làm I/O vòng vèo. Revision 61 tải WordPress chính thức một lần vào cache OneClick, lọc thành phần thừa, thêm asset đã xác minh, greedy largest-first theo unpacked byte vào đúng N ZIP; N FTP worker upload và N HTTPS worker checksum/metadata/native-extract trực tiếp. Đổi lại máy OneClick phải tải và upload core ở lần đầu, đồng thời giữ cache lớn hơn; các lần sau dùng lại theo fingerprint/SHA-256. Bridge vẫn fail-closed và finalize chỉ chạy khi đủ HMAC checkpoint | ACCEPTED_SUPERSEDES_ADR040_SPLIT_MERGE_AND_ADR041_SERVER_FETCH |
| ADR-046 | Giải nén/ghép cây WordPress ở máy OneClick rồi upload thẳng final files bằng đúng N persistent FTP connection; hosting chỉ xác minh manifest | Hosting thật cho thấy native ZIP extraction vẫn là nút thắt, trong khi người dùng chấp nhận truyền khoảng 100 MB. Revision 63 đổi 30 MB/15 archive thành khoảng 118 MB/4.747 final file nhưng loại toàn bộ source decompress trên hosting. Cân bằng dùng `size + 64 KiB/file` để phân phối cả byte và FTP round-trip; tiny signed manifests giữ exact path/size/SHA-256, symlink/traversal guard, stage isolation và HMAC gate trước finalize. Trade-off là tốn thêm bandwidth/STOR; cache local giúp lần sau không giải nén lại khi fingerprint không đổi | ACCEPTED_SUPERSEDES_ADR045_FOR_ACTIVE_PIPELINE |

---

## 19. Change Protocol

Mỗi pull request/lần chỉnh sửa phải:

1. Nêu capability đang thay đổi.
2. Mở file từ File Map liên quan, không quét rộng nếu không cần.
3. Giữ nguyên Security Invariants hoặc thêm ADR nếu buộc phải đổi.
4. Chạy test tương ứng.
5. Cập nhật:
   - Context Capsule và README revision.
   - File Map.
   - Current Implementation Snapshot.
   - Test Matrix.
   - Decision Log nếu có quyết định mới.
   - Change Log bên dưới.

### Mẫu Change Log entry

```text
YYYY-MM-DD | README rev N | Loại thay đổi
- Changed: ...
- Files: ...
- Tests: ...
- Security impact: none / mô tả
- Next: ...
```

---

## 20. Change Log

### 2026-08-27 — README revision 65 — Phát hành bản nhân viên lên GitHub

- Repository: `https://github.com/LuongVanDuy/oneclick-dev-server`, lịch sử `main` được thay bằng snapshot sạch của bản hiện tại theo yêu cầu người dùng; các nhánh code cũ bị xóa sau khi `main` mới push thành công.
- Distribution: Source, README, asset đã pin và đúng một `build/bin/oneclick-dev-server.exe` được giữ để nhân viên tải/chạy.
- Privacy boundary: Toàn bộ `data/*` trừ build-boundary `data/go.mod`, website/database/backup/report/runtime, cache, diagnostics, virtual environment, dependency và secret file pattern bị loại khỏi Git.
- Artifact: Windows EXE revision 64, 42.406.400 byte, SHA-256 `BD2A30BABDF1C86A48584AD7DD7CCA41B525317CB3D4B7964E8413B088020F45`.

### 2026-08-25 — README revision 64 — Luồng FTP nằm trong Cấu hình nâng cao

- UI-only: Chuyển nguyên trường `Luồng FTP`, nút `Đo tối đa` và trạng thái đo vào `Cấu hình nâng cao`; không đổi ID, event, API hoặc logic cài đặt.
- Files: `fresh_ui.py`, `README.md`, rebuilt single EXE.
- Artifact: `build/bin/oneclick-dev-server.exe`, 42.406.400 byte, SHA-256 `BD2A30BABDF1C86A48584AD7DD7CCA41B525317CB3D4B7964E8413B088020F45`.

### 2026-08-25 — README revision 63 — Giải nén local, upload thẳng bằng 15 FTP connection

- User decision: Dung lượng truyền khoảng 100 MB không phải vấn đề; ưu tiên loại thời gian giải nén WordPress trên hosting.
- Changed package: WordPress official ZIP và ba asset đã pin checksum được lọc/ghép thành cây final path ngay trong cache OneClick. Deep Bricks paths dùng Windows long-path I/O; cache hợp lệ được dùng lại theo schema/fingerprint/worker count.
- Changed transfer: Cấu hình 15 tạo đúng 15 nhóm cân bằng theo `byte + 64 KiB/file`; mỗi worker giữ một FTP/FTPS connection xuyên suốt và `STOR` trực tiếp file của nhóm vào stage. Log phát liên tục số byte, file, worker hoàn tất và file hiện tại.
- Hosting work: 15 tiny control ZIP chỉ mang JSON manifest. Authenticated HTTPS bridge tạo directory, kiểm tra không traversal/symlink/duplicate/`wordpress/` prefix, xác minh exact size + SHA-256 từng final file, đặt quyền rồi yêu cầu đủ HMAC checkpoint trước finalize. Không tải, ghép hoặc giải nén source ZIP trên hosting.
- Files: `internal/recoverytool/tool/src/wpclean/fresh_install.py`, test mục tiêu, `README.md`, rebuilt single EXE.
- Tests: `4 passed, 9 deselected` trong 3,55 giây, gồm local tree, exact 15 persistent FTP connections, bridge contract và PHP 8.4 lint. Wails production build pass; không live-run hosting.
- Artifact: `build/bin/oneclick-dev-server.exe`, 42.406.400 byte, SHA-256 `BEF7513F71AD53E5BA0AB185EDFF1CA721478D465CEF09A960605228D0FA9214`; build/bin chỉ có một EXE và không còn test cache.
- Next: User chạy lại một website với worker 15; live-pass khi log chuyển sang upload trực tiếp file, không xuất hiện source ZIP/thư mục `wordpress` trung gian và thời gian tổng giảm so với revision 61.

### 2026-08-25 — README revision 62 — Popup Cài mới WP cập nhật log trực tiếp

- User evidence: Pipeline 15 ZIP độc lập revision 61 đã work trên hosting thật.
- UI: Bỏ heading `Cài mới WordPress` và mô tả `Website mới, theme và plugin tiêu chuẩn.` bên trong iframe vì shell OneClick đã hiển thị ngữ cảnh này.
- Live detail: Giữ tên website đang mở trong state; mỗi lần `/api/fresh-installs` trả dữ liệu sẽ render lại popup. Poll chuyển từ interval sang chuỗi `setTimeout` 1,2 giây để không tạo request chồng nhau; khi lỗi retry 2 giây chỉ lúc còn job/popup cần theo dõi. Log tự theo dòng cuối nếu user đang ở cuối, nhưng không giật xuống khi user chủ động cuộn lên; vùng log dùng `aria-live=polite`.
- Skill impact: UI/UX Pro Max được dùng để giữ information hierarchy gọn, feedback liên tục, polling tuần tự và accessibility cho log động; không đổi style system hoặc chức năng cài đặt.
- Files: `internal/recoverytool/tool/src/wpclean/fresh_ui.py`, UI contract test, `README.md`, rebuilt single EXE.
- Tests: Chỉ chạy test UI mục tiêu: `1 passed, 11 deselected` trong 1,57 giây. Wails production build pass; không chạy lại pipeline/backend đã user live-pass.
- Artifact: `build/bin/oneclick-dev-server.exe`, 42.385.920 byte, SHA-256 `FB7F2B9AAE3F2D613909AB4E3332D1FCB8DC7DCAB1AD931770FC46619545916A`.
- Next: User mở popup trong lúc cài và xác nhận log tự cập nhật không cần đóng/mở lại.

### 2026-08-25 — README revision 61 — 15 ZIP độc lập, không ghép và không thư mục WordPress trung gian

- Root cause: Revision 60 vẫn cắt một resource ZIP thành part FTP, PHP phải `stream_copy_to_stream` ghép lại; core lại được hosting tải thành `.wordpress.zip`, giải nén vào `stage/wordpress`, promote rồi mới xử lý resource. Có worker nhưng pipeline vẫn ghi/đọc lại dữ liệu theo nhiều phase.
- Changed package: OneClick tải/cache fixed official `https://wordpress.org/latest.zip`, kiểm tra host redirect, ZIP root/traversal/symlink/count/size, bỏ license/readme/sample config, Akismet, Hello và theme mặc định. Core cùng Bricks/child/plugin được phân phối largest-first theo unpacked byte vào đúng số worker đã lưu; từng shard là ZIP hợp lệ và mọi entry đã là final path.
- Changed transfer/extract: Cấu hình 15 tạo đúng 15 ZIP, 15 FTP request đồng thời và 15 HTTPS request đồng thời. Mỗi PHP worker xác minh SHA-256 + metadata rồi `extractTo($stage)` toàn shard; không có arbitrary byte part, server merge, `.wordpress.zip`, `stage/wordpress`, `promote-core` hoặc `prepare-resource`.
- Safety: Root cleanup vẫn cần consent và chặn FTP root; official-host/TLS/cache checksum, per-shard SHA-256, traversal/symlink/count/size guard, random tokenized HTTPS bridge, HMAC checkpoint, DB ownership marker và permission `0755/0644/0600` giữ nguyên. Lỗi worker retry một lần rồi cleanup đúng stage/shard.
- Files: `internal/recoverytool/tool/src/wpclean/fresh_install.py`, test mục tiêu, `README.md`, rebuilt single EXE.
- Tests: Chỉ chạy `tests/test_fresh_install.py`: `12 passed` trong 2,75 giây, gồm exact 15 independent ZIP/final-path contract và PHP 8.4 lint. Wails production build pass; không live-run hosting.
- Artifact: `build/bin/oneclick-dev-server.exe`, 42.384.896 byte, SHA-256 `CA7909D277819963EF51AE83AE73F00132498FA36DE1B960BD601CDB67D52DB9`.
- Next: User mở revision 61, bấm thử lại site với worker 15 và gửi log/thời gian; expected log upload `15/15 gói` rồi extract `15/15 worker`, không xuất hiện file ZIP hợp nhất hoặc thư mục `wordpress` trung gian.

### 2026-08-25 — README revision 60 — Giải nén song song đúng max worker cấu hình

- Live evidence revision 59: `tongkhokhoathongminh.com` upload resource 8.166.116 byte từ 01:03:34 đến 01:03:36, nhưng stage hosting kéo dài đến 01:06:45; tổng 205 giây, trong đó server install khoảng 189 giây. Thư mục `stage/wordpress` là output của libzip chứ không phải FTP từng file.
- Changed orchestration: `prepare` tải WordPress một lần, validate và precreate directory. OneClick phát đồng thời đúng số worker cấu hình cho core shards, xác minh đủ HMAC checkpoint, promote core; sau đó làm tương tự với Bricks/plugin và chỉ `finalize` khi tất cả worker hoàn tất.
- Changed worker policy: Upload resource không còn tự giảm 15 xuống 8 khi ZIP nhỏ; cấu hình 15 luôn tạo 15 FTP part và 15 extraction request. Mỗi shard retry một lần rồi báo đúng worker lỗi, không tự hạ concurrency.
- Safety: Các worker chỉ ghi file list CRC32 không giao nhau; core/resource chạy thành hai phase để tránh race `wp-content`. Bridge/token random, HTTPS, ZIP metadata guard, resource SHA-256, database marker, permission `0755/0644/0600` và cleanup giữ nguyên.
- Tests: Target-only `tests/test_fresh_install.py` đạt `12 passed`, gồm PHP 8.4 lint và contract đa action/shard; Wails production build pass. Chưa live-run revision 60.
- Artifact: `build/bin/oneclick-dev-server.exe`, 42.384.384 byte, SHA-256 `3A2E7DEEE9C1321540E1A294814565FF3A9F82087E39FAED873E468A079593EE`.
- Next: User chạy revision 60 với worker 15 và gửi tổng thời gian/log nếu worker bị lỗi hoặc vẫn vượt 90 giây.

### 2026-08-25 — README revision 59 — Giải nén WordPress nguyên khối bằng native libzip

- Root cause: 15 FTP worker chỉ dùng cho gói resource; khi `.oneclick-stage-*` tăng ở `wp-includes`, upload đã xong và PHP bridge đang `getStream` + `stream_copy_to_stream` tuần tự hàng nghìn file. `wp-admin` nhỏ hơn và `wp-content` core đã được lọc nên hoàn thành nhanh hơn.
- Changed: Bridge vẫn duyệt metadata để chặn sai root, traversal, symlink và giới hạn ZIP, nhưng không mở/copy từng entry. `ZipArchive::extractTo()` giải nén native toàn gói; core được chuyển khỏi thư mục `wordpress/`, sau đó mới xóa Akismet, Hello, theme Twenty, docs/sample config. Resource package cũng dùng native extraction.
- Safety: URL WordPress cố định, TLS verify và size bound giữ nguyên; resource ZIP vẫn SHA-256. Native output tiếp tục qua symlink guard metadata và permission normalization `0755/0644`, `wp-config.php` `0600`. Không chạy shell và không cần user login hosting.
- Tests: Chỉ chạy test mục tiêu `tests/test_fresh_install.py`: `12 passed` gồm PHP 8.4 lint, contract có `extractTo` và không còn `$zip->getStream($raw)`. Wails production build pass; không live-run hosting trong revision này.
- Artifact: `build/bin/oneclick-dev-server.exe`, 42.375.680 byte, SHA-256 `DE6323A9F3E88E714521EB5A4C3BD0DBBE84261B60546CD987C4C0D7F0C7BB25`.
- Next: User bấm `Thử lại` cho `tongkhokhoathongminh.com` và gửi thời gian tổng; nếu vẫn vượt mục tiêu 90 giây, log elapsed/stage sẽ tách download WordPress, native extraction và database để tối ưu đúng nút thắt tiếp theo.

### 2026-08-25 — README revision 58 — Xóa hồ sơ và pipeline mục tiêu 90 giây

- UI/lifecycle: Chi tiết `Cài mới WP` có nút `Xóa khỏi OneClick`; bắt buộc nhập đúng domain. Thao tác chỉ xóa JSON local, runtime secret và native credential, giữ nguyên source/database hosting.
- Performance: Bỏ FTP recursive wipe trước upload; PHP bridge đã xác thực tự dọn document root và kiểm tra lại trước cài. Package schema v4 bỏ source translation `.po`, mọi `.mo` ngoài tiếng Việt và test fixture của Bricks; package thực tế còn 8.166.116 byte so với khoảng 21 MB. WordPress core tiếp tục do hosting tải trực tiếp.
- Fixed 404: Root cause của `/wp-admin` và static asset là thư mục giải nén mode `0700`. Bridge mới áp directory `0755`, file `0644`, riêng `wp-config.php` `0600`, đồng thời loại symlink. Kiểm tra FTP sau đó cho thấy `public_html` live đang trống; revision 58 chưa tự chạy lại site khi user chuyển sang yêu cầu tính năng.
- Tests: Target-only theo yêu cầu: `tests/test_fresh_install.py` đạt `12 passed`; `go test ./internal/recoverytool` pass; Wails production build pass. Không chạy full suite hoặc live install.
- Artifact: `build/bin/oneclick-dev-server.exe`, 42.375.680 byte, SHA-256 `ECDBF061CFA90225F181B019E8D1CE7E5064E0B080A879A48510A4DECE51F493`.
- Next: Mở EXE revision 58, bấm `Thử lại` cho `tongkhokhoathongminh.com`, đo thời gian end-to-end và gửi log nếu vượt 90 giây; UI đã có stage/elapsed/current file để xác định phần chậm còn lại.

### 2026-08-25 — README revision 57 — Hosting tự tải WordPress và tự tạo database DirectAdmin

- Root cause: `tongkhokhoath_db` / `tongkhokhoath_user` đúng quy ước tên nhưng mật khẩu FTP không tự trở thành quyền MySQL; mọi candidate đoán tên đều bị `Access denied`. Retry cũ vẫn upload cả WordPress trước khi phát hiện DB sai nên vừa không thể tự sửa vừa tốn thời gian.
- Reference analysis: Chỉ phân tích tĩnh EXE user cung cấp, không chạy. EXE là PyInstaller không ký số; module cho thấy kiến trúc server-side WordPress fetch, PHP agent và DirectAdmin/cPanel database provisioning. File/module trích tạm đã xóa hoàn toàn sau khi tham khảo.
- Changed database: Bridge kiểm tra candidate trước, sau đó dùng credential hosting trong request HTTPS để gọi `CMD_API_DATABASES` qua DirectAdmin loopback port 2222, tạo exact suffix DB/user và thử MySQL lại. Chi tiết panel/MySQL được tách rõ khi lỗi; không còn đóng mysqli failed object.
- Changed performance: Cache schema v3 chỉ chứa Bricks/Bricks Child/Duy Anh Web Pro; hosting tải `https://wordpress.org/latest.zip` trực tiếp sau khi DB sẵn sàng. Core không còn download/ghép/upload từ Windows; Akismet, Hello Dolly, Twenty*, docs và sample config bị bỏ trong lúc giải nén.
- Safety: DirectAdmin HTTP fallback chỉ là `127.0.0.1` cùng hosting; credential không gửi qua HTTP Internet. WordPress download bắt buộc HTTPS/TLS verification; core/resource ZIP có traversal/symlink/file-count/unpacked-size guard; resource vẫn SHA-256 và random part/bridge token.
- Tests: Chỉ chạy test mục tiêu theo yêu cầu: `tests/test_fresh_install.py` đạt `11 passed`, gồm PHP lint. Wails production build pass; cache Python và toàn bộ thư mục phân tích EXE đã dọn; `build/bin` chỉ có một EXE.
- Artifact: `build/bin/oneclick-dev-server.exe`, 42,369,024 bytes, SHA-256 `2D8DE643BD7072A8C6065908ADBE6AF0775C699C4790CC2583EDFEE9759F1E7A`.
- Next: Mở revision 57 và bấm `Thử lại` cho `tongkhokhoathongminh.com`. Xác nhận log đi qua database trước, sau đó `Hosting đang tự tải và cài WordPress`; nếu panel login khác FTP login, log sẽ nói rõ DirectAdmin từ chối để bổ sung credential riêng ở revision sau.

### 2026-08-25 — README revision 56 — Package WP gọn, upload 15 luồng và sửa mysqli closed

- Performance: Bump cache schema để tạo lại package slim; bỏ `akismet`, `hello.php`, mọi theme Twenty*, readme/license và config mẫu. Chỉ core runtime, Bricks, Bricks Child và Duy Anh Web Pro được upload; cache dùng lại ở các lần sau.
- Transfer: ZIP được chia theo số worker FTP đã lưu (hosting hiện tại đo 15, trần 16), mỗi phần dùng connection riêng. Log hiển thị phần hiện tại, số phần và tổng byte; bridge ghép đúng thứ tự, xóa part và xác minh SHA-256 trước giải nén.
- Fixed: Bỏ `mysqli->close()` sau connection failure vì PHP có thể đã đóng object; đây là nguyên nhân `mysqli object is already closed` khi bridge thử candidate database tiếp theo.
- Safety: Tên bridge/archive/part vẫn random; directory allowlist chỉ nhận đúng các part đã pin, archive hoàn chỉnh phải khớp SHA-256. Stage/archive/part được cleanup khi thành công hoặc lỗi; destructive wipe/checkpoint/HTTPS token giữ nguyên.
- Tests: Chỉ chạy test mục tiêu theo yêu cầu: `tests/test_fresh_install.py` đạt `11 passed`, gồm PHP lint. Wails production build pass; thư mục test Python đã dọn, `build/bin` chỉ có một EXE.
- Artifact: `build/bin/oneclick-dev-server.exe`, 42,363,904 bytes, SHA-256 `B38FD1F7B4AA51927416A67041C8424FED2642AE77D2EA6BB23C571ACE070519`.
- Next: Mở revision 56, bấm `Thử lại` cho `tongkhokhoathongminh.com`. Lần đầu cache slim sẽ được tạo; theo dõi log upload có `15 luồng FTP` và `x/15 phần xong`, sau đó xác nhận bridge qua bước database.

### 2026-08-24 — README revision 55 — Đồng bộ Cài mới WP, log rõ và tự sửa database

- UI: Chỉ sửa màn `Cài mới WP`; dùng layout đồng bộ Khôi phục với KPI, timestamp đồng bộ, toolbar và card full-width. Popup bỏ mô tả dài, chỉ giữ trường chính; host/path/protocol/port/db host/table prefix chuyển vào `Cấu hình nâng cao`. Chiều cao button 36 px và mọi text từ 14 px.
- Credential: Form còn một `Mật khẩu FTP & quản trị`; backend ép database/admin password bằng FTP password và native vault lưu cùng giá trị. Tên database/user mặc định `<ftp-user>_db`/`<ftp-user>_user`; hồ sơ có lỗi Access denied mở Sửa sẽ tự đặt lại hai tên theo FTP.
- Database recovery: PHP bridge thử cấu hình đã lưu, sau đó thử `<ftp-user>_db` + `<ftp-user>_user` và exact FTP user; chỉ mở kết nối, không tự tạo/xóa database. Khi thành công, profile cập nhật đúng DB name/user thực tế. Lỗi cuối nói rõ mở Sửa để nhập dữ liệu DirectAdmin.
- Log: Job log thêm giờ, stage, phần trăm và file/path hiện tại. Ghép package báo mỗi 100 file; upload báo tên archive và MiB đã tải/tổng; chi tiết hiển thị thời gian chạy, tín hiệu cuối và 30 log. Bridge có heartbeat 5 giây trong lúc hosting giải nén/cài để UI không đứng im ở 70%.
- Targeted verification only: Python/PHP fresh installer `11 passed`; Go `TestFresh*` pass; generated fresh JavaScript syntax pass; frontend UI contract/TypeScript/Vite pass. Không chạy full suite, startup smoke hoặc live hosting/database test.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 42.356.736 byte, SHA-256 `46C089C8F174E463A05D01AB7CB74B5B2BF3C6DFDE0D2138DBB2427FE2491D4D`. `.venv`, `__pycache__` và `.pytest_cache` dùng cho test đã được xóa trước final build.
- Next: User chạy EXE revision 55 và bấm `Thử lại` cho `tongkhokhoathongminh.com`. Nếu hosting không dùng quy ước `tongkhokhoath_db` / `tongkhokhoath_user`, mở `Sửa` và nhập đúng hai tên từ DirectAdmin rồi lưu lại.

### 2026-08-24 — README revision 54 — Nhập nhanh, đo luồng FTP và tự dọn hosting

- Changed quick form: Nhập domain sẽ đồng bộ tên website, URL HTTPS, FTP host, DirectAdmin path `/domains/<domain>/public_html` và `admin@<domain>`. FTP username/password tự điền database name/user/password; database/admin vẫn sửa được khi hosting cấp thông tin riêng.
- FTP capacity: Thêm `Đo tối đa`, probe theo các mốc tới 16 connection và áp đúng số kết nối đã xác nhận. Probe chỉ login, `CWD`, `NOOP`, đóng session; không list/transfer/delete file, không tạo profile và Go proxy không ghi credential bundle cho endpoint probe.
- Explicit hosting cleanup: Form có checkbox bắt buộc ghi rõ nội dung cũ sẽ bị xóa. Backend persist consent, từ chối remote path `/`, dọn cuốn chiếu/reconnect-safe chỉ bên trong exact website root, giữ chính root và `.well-known`, rồi list lại trước upload. Vì vậy `cgi-bin`, `index.html` và source cũ không còn chặn fresh install; profile revision 53 thiếu consent sẽ mở form Sửa trước khi retry.
- Security/secret: Host/database/admin defaults cũng được backend áp dụng, không chỉ JavaScript. Password vẫn chỉ ở native vault/runtime secret; probe không persist hoặc trả secret. Database vẫn fail closed nếu không trống hoặc checkpoint không thuộc install ID.
- Targeted verification only: Python/PHP fresh installer `11 passed`; Go fresh proxy/native-vault/probe routing pass; generated fresh JavaScript syntax pass; frontend UI contract/TypeScript/Vite pass; hidden EXE startup responsive 6 giây và cleanup process pass. Không chạy full suite và không dùng credential thật/kết nối/xóa hosting.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 42.351.616 byte, SHA-256 `81B732678FED8F260E53BDAD885AFEAE7B17CC2B8FA223B1FCCEB76BBEA7B7F8`.
- Next: User mở revision 54, vào `Cài mới WP`, chọn `Sửa` hồ sơ `tongkhokhoathongminh.com`, kiểm tra checkbox dọn hosting rồi `Lưu và thử lại`; gửi lịch sử nếu database/HTTPS/PHP hosting còn chặn.

### 2026-08-24 — README revision 53 — Cài WordPress mới bằng một gói tốc độ cao

- Changed product/UI: Thêm menu cấp cao `Cài mới WP`. Màn riêng có KPI, danh sách site đã tạo, trạng thái job, lỗi, chi tiết và lịch sử JSON; form dùng cấu hình FTP tương tự Khôi phục, tự suy ra `/domains/<domain>/public_html`, thêm database và tài khoản quản trị. Site lỗi có nút `Sửa`; mật khẩu để trống reuse vault rồi lưu và thử lại. UI theo CRM compact của OneClick, full-width, font tối thiểu 14 px, button đồng bộ 36 px, focus/reduced-motion và mô tả ngắn.
- Bundled assets: Embed exact `bricks.2.3.10.zip`, `bricks-child.zip`, `duyanhwebpro-1.2.3.zip` cùng manifest SHA-256. Máy nhân viên không cần `D:\Wordpress Theme`; đổi source asset sẽ đổi bundle hash và yêu cầu nạp bundle mới nhưng không cài lại Python/thư viện.
- Fast pipeline: WordPress chính thức dùng cache 24 giờ; core + ba asset được kiểm tra path/symlink/duplicate/size/checksum rồi ghép/cache thành một ZIP không chứa secret. Mỗi site chỉ upload một archive lớn và một PHP bridge nhỏ, hosting giải nén/cài database, tạo `wp-config.php`/salt, bật permalink, Bricks Child và Duy Anh Web Pro, sau đó xóa archive/bridge. Lượt sau reuse package cache.
- Security: Bắt buộc URL HTTPS trùng domain; remote root fail closed nếu có nội dung ngoài `.well-known`/`.ftpquota`. PHP bridge tên random, token header 256-bit, xác minh SHA-256 archive và kiểm tra ZIP traversal lần hai. Database marker gắn `installId` cho retry, từ chối database có bảng hoặc marker của phiên khác. FTP/DB/admin password chỉ đi qua loopback request, native credential vault, protected runtime secret và HTTPS bridge body; profile/history/log không chứa secret.
- Lifecycle/data: Profile tại `workspace/fresh-installs/sites/*.json`, audit tại `workspace/fresh-installs/history.jsonl`; recovery project/state không bị trộn. Engine ẩn được giữ nếu fresh job đang chạy khi app đóng, và app sau có thể reattach; idle shutdown vẫn dọn runtime secrets/server state.
- Targeted verification only: Python/PHP `6 passed`, gồm create/edit không persist password, bundle thực, bridge auth/fail-closed và PHP 8.4 lint; Go targeted bundle/proxy/native-vault/blank-secret merge/runtime-secret/active-job lifecycle pass; frontend UI contract + TypeScript + Vite pass. Local embedded smoke `/fresh` và `/api/fresh-installs` HTTP 200/private marker pass rồi shutdown sạch. Không dùng credential thật, không kết nối hoặc mutate hosting/database.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 42.339.840 byte, SHA-256 `663EF393254E30904109CC1EB534709BD9D967EB4EE6663245ACC55FF5AB1FF8`. Hidden startup smoke responsive và đã dừng đúng PID; không còn OneClick/Python process.
- Next: User tạo một subdomain HTTPS, để remote root và database trống, mở revision 53 > `Cài mới WP` > điền FTP/database/admin rồi chạy live test. Nếu hosting thiếu PHP `ZipArchive`/`mysqli` hoặc HTTPS/DNS chưa hoạt động, UI phải fail rõ và không ghi đè dữ liệu cũ.

### 2026-08-24 — README revision 52 — Đổi “cài Python” thành “khởi động môi trường” khi runtime đã có

- Evidence: `data/tools/wp-clean-rebuild/runtime/.venv/Scripts/pythonw.exe` vẫn tồn tại và giữ thời gian sửa từ 16:49; UV cache cũng còn. Thông báo cài lại xuất hiện vì `synced-version` so với hash toàn bundle, nên mỗi EXE chứa code recovery mới đều yêu cầu chạy `Prepare`, dù Python không bị cài lại.
- Changed backend: `runtimeStatus` phân biệt runtime chưa có với runtime Python đã tồn tại nhưng bundle mới chưa sync. Trường hợp đã cài trả `start_required`, tiêu đề `Khởi động môi trường Khôi phục WP` và mô tả Python/thư viện đã có sẵn. Progress prepare dùng `Đang khởi động môi trường…`; chỉ runtime chưa có mới ghi `Đang cài Python…`.
- Changed frontend: Badge dùng `Sẵn sàng`/`Đang khởi động`; nút dùng `Khởi động môi trường`/`Đang khởi động…`. `Cần chuẩn bị` và `Cài môi trường` chỉ còn cho lần cài đầu hoặc runtime bị thiếu.
- Targeted verification only: `TestRuntimeStatusUsesStartMessageWhenPythonAlreadyExists` pass; Wails production build gồm UI contract/TypeScript/Vite pass. Không chạy full suite.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 21.251.072 byte, SHA-256 `2E3CB99B2DCF70D7CE8681E1DAC3BCBD6EAC664FEE5663D7B1A2F0C852F0F8A9`.
- Next: User mở revision 52 và xác nhận màn Recovery hiển thị trạng thái khởi động thay vì cài Python.

### 2026-08-24 — README revision 51 — Hiển thị đúng FTP worker và ghi chú xóa thủ công

- Live diagnosis: Project `phongkhamkbtoancau.com` lưu `workers=16`; FTP advisor đã kết nối ổn định 16/16 và global transfer ceiling là 32. API lúc user hỏi đang ở phase `wipe`, nhưng GUI vẫn hiện `LƯỢT 2/2 · 8 WORKERS` từ phase `manifest_verify` local trước đó.
- Root cause: Khi `_progress` đổi phase, code reset count/bytes/retry nhưng không reset `pass_index`, `pass_total`, `worker_count`, location và ETA. Vì vậy nhãn hash local bị giữ sang wipe; đây là lỗi telemetry, không phải upload bị cap 8.
- Changed: Reset toàn bộ metadata đặc thù phase khi phase chính đổi. Mỗi event upload cuốn chiếu phát `workers=transport.config.workers`, nên profile 16 sẽ hiện đúng `16 WORKERS`; wipe không còn nhãn pass/worker giả.
- Rebuild note: Gate xác nhận nhắc có thể dùng File Manager xóa trước nội dung trong đúng remote root để tiết kiệm thời gian, nhưng phải giữ `.well-known`, không xóa chính root và không xóa đồng thời sau khi app bắt đầu rebuild.
- Targeted verification only: 2 test mục tiêu pass; không chạy full suite. Wails production build gồm UI contract/TypeScript/Vite pass.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 21.250.048 byte, SHA-256 `AD937677DC14F7A7EED71E7C0DFF6ED81D654C2EBF2D6C08C7A2C20FFAC4288D`.
- Next: User mở revision 51 và xác nhận telemetry wipe/upload cùng rebuild note trên GUI thật.

### 2026-08-24 — README revision 50 — Xóa và upload FTP cuốn chiếu

- Root cause/performance: `wipe_remote_root_with_reconnect` MLSD toàn bộ cây vào hai list rồi mới DELE/RMD; `_upload_tree_with_reconnect` cũng `rglob` và stat toàn bộ local tree rồi mới tạo worker. Thời gian quét bị cộng nối tiếp với thời gian truyền.
- Changed wipe: Duyệt một thư mục, xóa ngay file đã phát hiện, đệ quy thư mục con rồi RMD hậu thứ tự. Không còn phase `wipe_inventory` toàn cây; vẫn giữ `.well-known`, safe-child/root guard, reconnect/idempotent delete, permission recovery và root-only verification cuối.
- Changed upload: Worker FTP khởi động trước inventory; file local vừa tìm thấy được đưa ngay vào hàng đợi và STOR song song với `rglob`. Kích thước file được stat đúng một lần và chuyển cùng work item; tổng file/byte được chốt khi discovery hoàn tất.
- FTP constraint: FTP chuẩn chỉ có `DELE` cho file và `RMD` cho thư mục rỗng, không có lệnh portable xóa đệ quy thư mục không rỗng. Revision này loại thời gian chờ inventory riêng nhưng vẫn xóa từng file để tương thích mọi hosting.
- Progress: Trong lúc tổng đang tăng, GUI giữ progress bar theo mốc stage để không nhảy 100% rồi lùi; số file đã tìm/upload vẫn cập nhật. Khi discovery chốt, progress dùng tổng chính xác.
- Targeted verification only: Theo yêu cầu user không chạy full suite. 5 test mục tiêu pass, gồm test đồng bộ chứng minh DELE xảy ra trước MLSD thư mục kế tiếp và STOR file đầu xảy ra trước khi inventory phát file thứ hai. Wails production build gồm UI contract/TypeScript/Vite pass.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 21.248.512 byte, SHA-256 `5CD4C690FD2686035FD3161FC83E9CBAA901D35E8C71F5782475AA37FD6916F2`. GUI cũ được đóng để thay EXE. Recovery API xác nhận không có job chạy (`needs-action` tại bước xác nhận rebuild), rồi engine cũ được shutdown sạch qua control endpoint để app kế tiếp nạp revision 50.
- Next: User mở lại app và chạy rebuild thật; live-pass khi bộ đếm xóa/upload tăng ngay, không còn màn chờ quét toàn bộ trước thao tác.

### 2026-08-24 — README revision 49 — Vừa quét vừa tải backup FTP

- Root cause/performance: `download_tree` gọi `list_files_recursive` để dựng toàn bộ danh sách theme/plugin/uploads, sau đó mới tạo worker futures. FTP không có recursive count chuẩn, nên thời gian MLSD bị cộng nối tiếp trước thời gian RETR và người dùng phải chờ lâu dù các file đầu đã được biết.
- Changed: Tách inventory thành `iter_files_recursive`; mỗi `RemoteFile` vừa phát hiện được submit ngay vào ThreadPool. Pending futures bị giới hạn tối đa 4× số worker để không cấp phát hàng chục nghìn Future; main thread thu kết quả ngay trong lúc iterator tiếp tục duyệt. Khi inventory kết thúc, code chốt danh sách/tổng byte rồi giữ nguyên hai lượt reconcile, xóa partial còn lỗi và complete stats/manifest.
- Progress/UX: Thêm phase `discover_transfer` với `files_found`, `files_completed` và `discovery_complete=false`; GUI hiển thị `Đang quét và tải backup`, không cho progress bar nhảy 100% rồi lùi khi tổng đang tăng. CLI/operator view cũng hiển thị đồng thời số tìm thấy và số đã tải; khi inventory chốt sẽ chuyển về progress chính xác.
- Regression: Test đồng bộ bằng `threading.Event` buộc file đầu phải tải xong trước khi iterator phát file thứ hai; test riêng xác nhận progress giữ phần trăm monotonic trong lúc tổng chưa chốt. Targeted `20 passed`; full embedded recovery `158 passed`.
- Verification: `npm run build` pass UI contract/TypeScript/Vite. Tất cả Go package pass; Windows chặn tên EXE test tạm của Source Editor nên package được compile thành binary riêng và 4/4 pass; `go vet` pass. Wails production build và hidden startup smoke responsive/cleanup pass.
- Security/reliability: Không đổi hosting mutation surface: chỉ MLSD/RETR, không upload/rename/delete/execute. Bounded queue giảm memory; relative RETR, zero-byte wrapper, resume, retry, reconcile, fail-closed integrity và manifest giữ nguyên.
- Artifact: Bốn `__pycache__` do pytest tạo đã được loại trước final build. Chỉ một `build/bin/oneclick-dev-server.exe`, 21.243.904 byte, SHA-256 `DBE5A834E109D79B4551B4094BAE39E9F9EF06273C9C1E18B1D6F953A2C2CA5A`.
- Next: User chạy lại backup thật và xác nhận theme/plugin bắt đầu tăng số đã tải ngay khi số file tìm thấy còn tiếp tục tăng.

### 2026-08-24 — README revision 48 — Sửa TypeError wrapper tải FTP

- Root cause: Revision 47 mở rộng `FTPTransport._download_one` từ 6 lên 7 tham số với `preserve_partial_on_failure`. Module `zero_byte_fix` monkey-patch method này nhưng vẫn giữ chữ ký cũ và chỉ chuyển tiếp 6 tham số. `download_tree` luôn gọi đủ 7 tham số, nên wrapper ném `TypeError` trước khi downloader có thể `CWD` hoặc gửi `RETR`; các unit test revision 47 gọi trực tiếp 5–6 tham số hoặc mock method nên chưa đi qua đúng đường lỗi thật.
- Changed: `_zero_byte_safe_download_one` nhận `preserve_partial_on_failure: bool = False` và chuyển tiếp nguyên vẹn tới base downloader. Logic tạo/truncate file 0-byte không đổi.
- Regression: Thêm test gọi exact 7 positional args qua wrapper với file non-zero, client giả xác minh `CWD /root`, `RETR file.bin`, nội dung local và flag được chấp nhận. FTP/zero-byte/log targeted `18 passed`; full embedded suite `156 passed`.
- Verification: Frontend UI contract/TypeScript/Vite, toàn bộ Go package, Source Editor 4/4, `go vet ./...` và Wails production build pass. Test cache/runtime tạm đã xóa khỏi embedded source.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 22.778.368 byte, SHA-256 `5286219B0BA9331EE338436C3CBE9B543712ECEB902652C92101DD4F0DA21216`.
- Next: User mở revision 48 và retry backup `phongkhamkbtoancau.com`; live-pass khi không còn TypeError và file counter downloaded/skipped tăng.

### 2026-08-24 — README revision 47 — Sửa FTP quét được nhưng tải 0 file

- Root cause evidence: `phongkhamkbtoancau.com` login, `CWD /public_html`, recursive listing và phép thử 8–16 connection đều pass; ổ D có quyền Modify. Phiên thật lại kết thúc `uploads 0/5.681` và `themes 0/2.315`, thư mục backup không có file. Downloader mở connection ở `/` rồi gửi `RETR /public_html/...`; hosting chấp nhận absolute path cho `MLSD` nhưng không cho `RETR`. `error_perm 550` đi thẳng qua generic failure và progress logger bỏ trường `error`, nên UI chỉ hiện “Một file chưa tải được”.
- Changed: Mỗi FTP worker cache exact remote tree root, `CWD` lại sau connection/reset rồi gửi `RETR` tương đối. Nếu server legacy từ chối relative, downloader truncate về checkpoint và fallback đúng một lần sang absolute path. Connection-reset retry/resume giữ nguyên; permanent `550` không bị lặp nhiều vòng. Progress/log mới giữ server reason tối đa 500 ký tự và redaction runtime secret.
- Tests: FTP/log targeted `14 passed`; full embedded recovery `155 passed`; relative-only server, absolute-only fallback, reconnect/resume, failure cleanup, reconciliation và secret-redaction đều pass. Frontend UI contract/TypeScript/Vite, toàn bộ Go package, Source Editor 4/4, `go vet ./...` và Wails production build pass.
- Safety: Chỉ đọc FTP trong phiên chẩn đoán; không upload, rename hoặc delete hosting. Theo xác nhận user có thể chạy lại, active local backup engine được gọi graceful shutdown rồi OneClick đóng trước khi thay EXE. Partial backup chỉ có các directory rỗng và được workflow retry quản lý.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 22.772.224 byte, SHA-256 `630879B723FFD0C25EF68CE343DB7BA084A277CA138E6B8301529DE54D29BB1A`.
- Next: User mở revision 47 và chạy lại `phongkhamkbtoancau.com`; live-pass khi `uploads/themes/plugins` có downloaded/skipped > 0 và backup integrity hoàn tất.

### 2026-08-24 — README revision 46 — Đồng bộ layout Khôi phục theo màn Website

- Changed: Runtime source chỉ đổi HTML/CSS của embedded recovery dashboard: bỏ toolbar lồng dư thừa, đưa KPI và trạng thái đồng bộ vào một thanh ngang, gom tìm kiếm/action cạnh tiêu đề danh sách, ẩn table header và hiển thị mỗi dự án thành card full-width giống nhịp bố cục màn Website. Có responsive riêng tại 1040/820 px, font hiển thị giữ tối thiểu 14 px và action cao 36 px.
- Functional impact: Không đổi API, route, state, workflow, connection, backup, scan, rebuild, delete hoặc hosting action. Toàn bộ khối `<script>` trước/sau có cùng SHA-256 `14B709F60075EAFFCF950F5662E4C024683282EBC993AAC27CA8467BEB94BC52`, xác nhận JavaScript điều khiển không đổi.
- Tests: Full embedded recovery suite `152 passed`; frontend UI contract/TypeScript/Vite pass; mọi package Go pass, Source Editor 4/4 pass dưới tên binary test riêng do Windows khóa tên mặc định; `go vet ./...` và Wails production build pass. Cache test không được đóng gói.
- Safety: Test chỉ dùng fixture/temp, không kết nối hoặc mutate hosting/source/database/VM/recovery workspace. Trước khi thay EXE, không còn tiến trình OneClick/Python đang chạy nên không cắt job khôi phục.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 22.750.208 byte, SHA-256 `9BE6F92F9A02236E9F10A5029DBC14CDD1082D2CA1879286702CA08D8B322F39`.
- Next: User mở revision 46 và xác nhận trực quan màn Khôi phục so với màn Website.

### 2026-08-24 — README revision 45 — Sửa dứt điểm engine Khôi phục chờ sai 35 giây

- Root cause: Revision 44 xóa đúng tên legacy khỏi HTML nhưng Go health probe vẫn tìm chuỗi `WP CLEAN REBUILD` trong body. Python server thực tế khởi động được, nhưng probe luôn trả false nên OneClick chờ đủ 35 giây rồi kill engine và báo chưa phản hồi.
- Changed: HTML root trả private machine-readable header `X-OneClick-Recovery: 1`. Probe yêu cầu đồng thời loopback HTTP 200, exact server family, private header và API path; không còn phụ thuộc wording/theme. Server giả có tên/body đúng nhưng thiếu private header bị từ chối.
- Lifecycle: Command cài/kiểm tra hidden tự kết thúc. Khi đóng app và recovery idle, proxy/engine bị dừng và runtime secret được dọn; nếu job recovery đang chạy, engine được giữ có chủ đích để không cắt giữa backup/rebuild và OneClick sẽ nối lại khi mở lần sau.
- Tests: Recovery Go targeted pass; Python GUI targeted `12 passed`, full engine `152 passed`; mọi Go package khác pass, `sourceeditor` được compile thành binary tên riêng để vượt Windows lock tên temp và 4/4 test pass; `go vet ./...`, frontend UI contract/TypeScript/Vite và Wails production build pass.
- Live evidence: Revision 45 sync bundle `9be8f8316e688b11…`, tạo `server-state.json`; exact upstream `127.0.0.1:59797` trả HTTP 200, `Server: WPCleanGUI/1.0 Python/3.13.15`, private marker `1`, có `Khôi phục WordPress`, không có tên legacy. Không kết nối hoặc mutate hosting/source/database/VM.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 22.738.944 byte, SHA-256 `FE999D8F125EEEA4F684AE9D8D02BC3C3C6B19F512DBD1C7E50F1E84985E09F2`.

### 2026-08-24 — README revision 44 — Đồng bộ giao diện Khôi phục với OneClick

- Changed: Chỉ thay lớp trình bày của OneClick và embedded recovery dashboard: dùng lại CRM palette sáng/typography/spacing/border của ứng dụng, danh sách và panel dùng toàn bộ chiều rộng content, cỡ chữ hiển thị tối thiểu 14 px, button giữ chuẩn 36 px, terminal được giữ như log surface riêng và bỏ toàn bộ text tên công cụ legacy khỏi UI/title.
- Functional impact: Không đổi JavaScript, API route, state, workflow, connection, backup, scan, rebuild, delete hoặc hosting action. Tiến trình Python khôi phục đang hoạt động của user không bị dừng, restart hay smoke-run chồng lên.
- Tests: `npm run check:ui` pass font ≥14 px/full-width/button/focus/reduced-motion; `npm run build` pass TypeScript/Vite; recovery HTML visual contract pass; full embedded recovery suite `151 passed`; `go test -p 1 ./... -count=1`, `go vet ./...` và Wails production build đều pass.
- Security: Test dùng fixture/temp riêng; không kết nối hosting và không mutate deployment, source, database, VM hay recovery workspace thật.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 22.734.848 byte, SHA-256 `8356015852452785F1C2EFBEA8B5423EEC4C8DB4B18C1EDB7117A45A4C6EB081`. Không chạy startup smoke revision 44 vì workflow recovery thật đang hoạt động; user đóng/mở lại app sau khi workflow tới điểm dừng an toàn để tải giao diện mới.

### 2026-08-24 — README revision 43 — Xóa Windows long-path và live cleanup hoàn tất

- Root cause: Sau khi legacy path đã rebase đúng, `shutil.rmtree` vẫn dùng Win32 path thường. Backup WordPress còn 27 JPG có full path 298–330 ký tự; lượt `rmdir` thư mục tháng nhận WinError 145 vì các file long-path chưa được dọn hết.
- Changed: Mọi local delete trên Windows chuyển absolute target sang `\\?\<drive>\...` hoặc `\\?\UNC\...` sau containment check. Directory delete chạy tối đa 8 lượt với exponential bounded delay cho WinError 5/32/145 và EACCES/EBUSY/ENOTEMPTY; mỗi lượt tiếp tục dọn phần còn sót, còn lỗi khác vẫn fail closed ngay. Symlink/outside-root guard giữ nguyên.
- Tests: Thêm test tạo file thật có full path >260 ký tự và xóa thành công; test giả lập lượt đầu WinError 145 rồi retry pass. Targeted `16 passed`, full WP Clean Rebuild `151 passed`, live embedded pass, Wails production build và hidden GUI smoke pass.
- Live evidence: Sau khi user cho phép đóng app, EXE revision 43 được atomic-swap theo checksum. Chạy đúng local-delete engine trên workspace thật: `japangreenpower.com.vn` hoàn tất, sau đó `x3sales.vn` hoàn tất; `sites/backups/repairs` còn 0 mục, report directory của cả hai host không còn, runtime secret của hai profile không còn. Không có kết nối hoặc mutation hosting trong thao tác.
- Files: `internal/recoverytool/tool/src/wpclean/project_delete_command.py`, `tests/test_project_delete.py`, `README.md` và rebuilt single EXE.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 21.205.504 byte, SHA-256 `1338DFA9E6B56059BC10D2B679C03D0BE58E8BDAD3817FFE25311C751139FA20`, PE GUI subsystem 2; smoke PID `26640` responsive và được đóng đúng PID.
- Next: User mở OneClick và xác nhận menu Khôi phục WordPress không còn hai project cũ.

### 2026-08-24 — README revision 42 — Rebase dứt điểm đường dẫn dữ liệu cũ sau import

- Root cause: Import đã sao chép đủ file nhưng `reports/<host>/operator-state.json` vẫn giữ `backup_root` tuyệt đối của app cũ. Lần xóa revision 40 lấy target đó, containment guard chặn đúng; trạng thái retry lại persist target/lỗi legacy nên giao diện tiếp tục nhắc `D:\DuyAnhWeb`.
- Changed: `_paths` chỉ chấp nhận backup root nằm trong `workspace/backups`. Absolute legacy path chỉ được lấy basename khi đúng `<host>` hoặc `<host>-<timestamp>`, sau đó rebase và ghi lại `operator-state.json`. Khi payload được load, mọi persisted delete target ngoài backup/report/repair root bị thay bằng local fallback, lỗi kỹ thuật cũ được xóa và trạng thái đổi thành có thể retry. API delete lặp lại cùng guard trước khi khởi động thread.
- Evidence: Import journal thực tế xác nhận 229.036 file, 32.278.821.356 byte, copied 229.036, skipped 0. Test tạo source canary ngoài workspace và cả hai stale path lớp operator/deletion; payload tự sửa sang local, xóa thành công bản OneClick, source canary giữ nguyên.
- Files: `internal/recoverytool/tool/src/wpclean/operator_wizard.py`, `gui_entry.py`, `tests/test_gui_server.py`, `README.md` và rebuilt single EXE.
- Tests: WP Clean Rebuild `149 passed`; targeted stale-path/self-heal/delete test `11 passed`; full Go/vet revision 41 pass và live embedded revision 42 pass. Production Wails build và hidden 7-second GUI smoke pass. Không gọi hosting, không xóa/ghi nguồn cũ, không mutate 4 deployment live.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 21.202.944 byte, SHA-256 `B3B823ED87B0C62BDBF72F53077EF86ED4DAA068408A5EDC8957741EDD3B47E7`, PE GUI subsystem 2; smoke PID `35856` responsive và được đóng đúng PID.
- Next: User mở revision 42, vào hai hồ sơ cũ và bấm `Tiếp tục xóa phần còn lại`; xác nhận source `D:\DuyAnhWeb` vẫn nguyên và danh sách local về trống.

### 2026-08-24 — README revision 40 — Xóa được dự án khôi phục cũ/chưa hoàn tất

- Root cause: Dashboard chỉ render nút xóa khi `completed=true`, backend cũng chặn hồ sơ chưa Final Verify PASS. API xóa chạy nền nhưng UI đóng chi tiết và thông báo hoàn tất ngay, khiến hồ sơ còn trên danh sách trông như không xóa được.
- Changed: Mọi hồ sơ WP Clean Rebuild đã lưu đều có `Xóa dự án local`; workflow đang chạy thì nút khóa và backend fail closed. Lần đầu phải nhập chính xác tên dự án. UI giữ màn chi tiết, hiển thị phase/error/retry, poll mỗi giây và chỉ báo thành công/đóng chi tiết sau khi profile thực sự biến mất.
- Local-only/security: Background task xóa theo thứ tự backup, reports, repairs, runtime secret rồi profile; path vẫn bị containment guard trong workspace. Go loopback proxy dọn luôn credential WP Clean Rebuild khỏi native OS vault khi API delete được engine chấp nhận. Không có request delete/upload/rename nào gửi tới hosting.
- Files: `internal/recoverytool/tool/src/wpclean/gui_entry.py`, `gui_server.py`, `gui_ui.py`, `gui_journal_entry.py`, `secret_runtime.py`, `tests/test_gui_server.py`, `internal/recoverytool/manager.go`, `proxy.go`, `recoverytool_test.go`, `README.md` và rebuilt single EXE.
- Tests: WP Clean Rebuild `149 passed`; test mới cover incomplete imported project delete, exact-name mismatch, active workflow block và runtime-secret cleanup. Full `go test -p 1 ./... -count=1`, `go vet ./...`, proxy credential-delete test, live embedded runtime extract/install/start/proxy/GET/shutdown, UI contract/TypeScript/Vite đều pass. Không dùng credential thật, không chạm hosting hoặc 4 deployment live.
- Artifact: `build/bin/oneclick-dev-server-r40.exe`, 21.198.336 byte, SHA-256 `049C9F64ED88D58989FB43EAEB56C857A0D1299F5DF326C045CFED51FDD4BC9A`, PE GUI subsystem 2; hidden 7-second startup responsive và đóng đúng smoke PID `31368`. Chờ thay vào tên chuẩn vì PID cũ `14512` vẫn giữ file đang chạy.
- Next: User đóng cửa sổ OneClick đang chạy; thay EXE revision 40 vào tên chuẩn, rồi mở lại và xóa hai dự án cũ bằng exact-name confirmation.

### 2026-08-24 — README revision 39 — Full WP Clean Rebuild embed và nhập dữ liệu cũ

- Changed: Thay màn recovery tự viết bằng complete `wp-clean-rebuild` dashboard chạy ngay trong content OneClick. Toàn bộ code/tests/scripts/docs/theme được embed vào single EXE, versioned extract trên application drive; không link/copy lúc runtime từ `D:\DuyAnhWeb` và không đóng gói `WP-Clean-Rebuild.exe` thứ hai.
- Runtime/UI: Nút `Cài môi trường` chạy `uv sync --locked --no-dev` một lần vào runtime riêng, hidden/no-window. Python server bind random `127.0.0.1`, không tự mở browser; Go xác minh exact server marker rồi dựng random loopback proxy, bỏ frame denial chỉ tại proxy và áp CSP/no-store/nosniff. Iframe có sandbox/title/no-referrer, loading/error/retry và reduced-motion.
- Settings import: Thêm native folder picker, pre-scan file/byte/profile/secret/conflict, explicit `Xác nhận sao chép`, progress file/byte, retry và result. Chỉ allowlist `sites/backups/reports/repairs/logs/.wpclean-cache`; từ chối link/reparse/special/path escape, tối đa 500.000 file/200 GB, không overwrite conflict, copy temp+sync+source identity verify, giữ source cũ nguyên vẹn và ghi journal không secret.
- Secret/lifecycle: Embedded profile payload luôn trả password rỗng. Profile mới/imported loại `password`; credential vào native OS vault, chỉ materialize thành restricted runtime secret khi engine chạy. Idle shutdown dừng engine/proxy và xóa secret directory/server state; job đang chạy được giữ để có thể reattach sau app restart bằng control token riêng.
- Real evidence: Read-only scan đúng workspace cũ nhận 229.036 file, 32.278.821.356 byte, 2 profile và 2 secret; không copy/xóa trong build session. Python suite `147 passed`; Go unit/root/secrets + `go vet ./...` pass; live integration extract/install/start/proxy/GET/shutdown/secret-cleanup pass; frontend UI contract/tsc/Vite và Wails production build pass.
- Files: `app.go`, `main.go`, `internal/recoverytool/`, complete `internal/recoverytool/tool/`, `internal/secrets/store.go`, `frontend/src/App.tsx`, `frontend/src/App.css`, generated bindings/dist, `README.md` và rebuilt single EXE.
- Security impact: External source và 4 live deployment không bị mutate. Imported backup/repair giữ original extensions để engine tương thích và phải coi là untrusted; OneClick không tự execute/open chúng. Password không còn persist trong profile JSON mới hoặc list payload; import copy-only có explicit consent/fail-closed guard.
- Artifact: Một file `build/bin/oneclick-dev-server.exe`, 21.161.472 byte, SHA-256 `B77B5075F8809F102F43F234C46953C62F9BAA68E8A972434330B5271934EE6E`, PE GUI subsystem 2; hidden GUI smoke 7 giây pass.
- Next: User mở revision 39, vào `Khôi phục WordPress` để xác nhận dashboard nhúng; nếu cần dữ liệu cũ thì vào Cài đặt, kiểm tra plan 32,28 GB và chủ động xác nhận copy khi đủ dung lượng.

### 2026-08-24 — README revision 38 — Source backup và malware scan read-only

- Changed: Hostname mới tự đề xuất DirectAdmin path `/domains/<domain>/public_html` (bỏ prefix `ftp.`); backend tự sinh cùng path khi input trống. Danh sách Khôi phục WP có CTA `Sao lưu & quét`, confirmation plan, tiến trình phase/file/byte, summary finding/critical, dữ liệu và retry.
- Backend: Refactor authenticated FTP/explicit FTPS session để dùng lại cho bounded recursive `MLSD`, EPSV với PASV fallback và 1–8 RETR workers. PASV server IP bị bỏ qua để ngăn FTP bounce; mọi command/name/type/size/depth/file/byte/time đều fail closed. Hosting chỉ nhận auth/list/retrieve/PWD/CWD/QUIT, không upload/rename/delete/execute.
- Evidence format: Remote bytes được SHA-256 trong lúc tải, lưu bằng content-addressed `blobs/<prefix>/<sha256>.ocblob` mode riêng, rồi đọc lại reverify. `manifest.json` giữ path/size/hash/blob; `scan-report.json` giữ bounded static findings cho PHP trong uploads, PHP ẩn, encoded eval/request shell và obfuscation. Không giữ original executable extension trên filesystem, không extract/run blob; partial directory bị xóa khi lỗi và final directory commit atomic.
- State/reliability: `recovery.json` chỉ thêm non-secret backup ID/counters/time/status; không lưu absolute backup path, password hoặc content. App restart phục hồi `backup_running` thành `backup_failed` có retry; sửa profile giữ evidence summary nhưng yêu cầu kiểm tra kết nối lại.
- Files: `app.go`, `internal/recovery/model.go`, `repository.go`, `connection.go`, `backup.go`, recovery tests, `frontend/src/App.tsx`, `App.css`, generated Wails bindings/dist và `README.md`.
- Tests: Full fake FTP pipeline recursive MLSD + two parallel RETR + neutral blobs + manifest/reverify + critical hidden-PHP scan pass; default path, crash recovery, profile metadata/no-secret, connection tests pass. `go vet . ./internal/...`, `go test -p 1 . ./internal/... -count=1`, Wails binding generation và `npm run build` gồm UI contract/TypeScript/Vite pass. Production build và final hidden 5-second startup smoke pass. Không dùng credential thật, không kết nối hosting thật và không mutate 4 deployment live.
- Security impact: Mở remote read/download chỉ sau user confirmation; malware bytes không được execute/extract và không mang original extension. Static rules có thể false positive/false negative nên kết quả là triage, không phải chứng nhận sạch. Database acquisition/rebuild/mutation vẫn khóa.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 12.700.672 byte, SHA-256 `67B2510F9CA5338608040A5E94A38BF2786748D365E780B32BD6DF655C482B1E`, PE GUI subsystem 2.
- Next: User test profile đã kết nối trên hosting thật, xác nhận auto path/backup/scan/result. Sau live-pass thiết kế database acquisition read-only không upload helper, rồi isolated clean rebuild với destructive confirmation riêng.

### 2026-08-24 — README revision 37 — Tích hợp module Khôi phục WordPress

- Changed: Thêm menu cấp cao `Khôi phục WP`, danh sách hồ sơ/trạng thái/lịch sử, form FTPS/FTP, sửa cấu hình, kiểm tra lại, mở exact data folder và xác nhận xóa hồ sơ. UI giữ CRM compact, font tối thiểu 12 px, action button 36 px và sidebar fixed.
- Backend: Thêm `internal/recovery` với JSON schema 1 riêng tại `data/recovery/recovery.json`, strict/size/project/history limits, atomic replace, canonical host/HTTPS/remote-path validation và read-only FTP control-channel test. FTPS explicit bắt buộc TLS 1.2+ và certificate verification; plain FTP cần `allowInsecure` tường minh. Test chỉ greeting/auth/`CWD`/`PWD`, không list/download/upload/delete/execute.
- Secrets: FTP/FTPS password lưu bằng native OS credential vault theo random project ID; list response chỉ có `passwordStored`, recovery JSON không có password/derived data path. Delete hồ sơ không xóa evidence directory và không chạm hosting.
- Files: `app.go`, `internal/recovery/*`, `internal/secrets/store.go`, `frontend/src/App.tsx`, `frontend/src/App.css`, generated Wails bindings/dist, `.gitignore` và `README.md`.
- Tests: Recovery repository/validation/no-secret/evidence-retention và fake FTP auth/path tests pass; App/recovery/secrets targeted tests, full vet, các package hiện có, sourceeditor direct test binary, Wails binding generation và `npm run build` pass. Production Wails build pass; elevated hidden 5-second startup smoke pass. Không dùng credential thật, không kết nối hosting và không mutate 4 deployment live.
- Security impact: Tăng bề mặt network outbound chỉ khi nhân viên bấm `Kiểm tra`; fail closed với certificate/auth/path lỗi. Không đưa engine browser cũ, plaintext password hoặc FTP-password-as-admin-password sang OneClick. Backup/scan/rebuild vẫn khóa cho tới phase isolation/confirmation sau.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 12.606.976 byte, SHA-256 `4E40E6FF246E654A65E6D2D8D6AB18B6B97DDF6C4E630EC3CD5F6EDABF43CB66`, PE GUI subsystem 2.
- Next: User mở `Khôi phục WP`, thêm một website bằng FTPS và kiểm tra kết nối. Sau khi pass, nối engine backup + manifest + malware scan ở chế độ read-only trước clean rebuild.

### 2026-08-24 — README revision 36 — Source Manager trong ứng dụng

- Changed: `Quản lý source` giờ mở màn CRM hai cột trong chi tiết website: breadcrumb/thư mục, file lock, trình sửa UTF-8, trạng thái unsaved, hoàn tác, save và reload khi conflict; `Mở bằng Explorer` giữ làm action phụ và dùng Windows ShellExecute ổn định hơn.
- Backend: Thêm exact-project list/read/save bridge. Mọi path phải nằm trong source đã lưu; khóa secret/key, `.git`, dependency, link/reparse, binary/extension lạ và file >2 MB. Save giữ bản gốc trong `data/source-edits`, kiểm tra expected SHA-256, temp sync/replace, xác minh checksum mới; JSON history chỉ ghi action/path/status, không ghi content.
- Files: `app.go`, `app_test.go`, `internal/sourceeditor/*`, `internal/state/*`, `internal/hostopen/open_windows.go`, `frontend/src/App.tsx`, `frontend/src/App.css`, generated Wails bindings và `README.md`.
- Tests: Tất cả Go package có test compile/run trực tiếp tuần tự và pass; `go vet . ./internal/...`, source list/read/save/backup/stale/traversal/secret/binary/link/dependency tests, App bridge + state audit test, Wails binding generation và `npm run build` gồm UI contract/TypeScript/Vite pass. Chỉ dùng source fixture tạm, không sửa 4 project thật.
- Security impact: Thêm host-write có consent theo ADR-033, không thêm guest-to-host write, mount hoặc auto-sync. Source public đang chạy không đổi sau save host; secret/content không vào state/log. Backup pre-save và stale-hash fail closed trước overwrite.
- Artifact: Chỉ một `build/bin/oneclick-dev-server.exe`, 12.544.000 byte, SHA-256 `7CB9A621641574A41518B1F223A9F03F69E781CE944D9AE8710983C82D941AFD`, PE GUI subsystem 2; hidden 5-second startup responsive và đóng sạch.
- Next: User test một file CSS/PHP không nhạy cảm trong Source Manager; sau phản hồi triển khai `Cập nhật website` bằng snapshot mới, health-check và rollback.

### 2026-08-24 — README revision 35 — Quản lý Source/Database và live backup

- Changed: Thêm khối Source/Database trong chi tiết từng website; mở exact source gốc, tạo/mở backup, progress/result, schema/table metadata và guard dừng public + completed backup trước khi thay database. Backend export đúng per-site working copy/schema, loại secret/link/special path, download allowlist, kiểm tra SHA-256/tar và ghi manifest local.
- Files: `app.go`, `internal/backup/`, `internal/hostopen/`, `internal/vm/guest.go`, `internal/vm/fileinfo_*`, `internal/state/`, `frontend/src/App.tsx`, `frontend/src/App.css`, generated Wails bindings/dist, `README.md`, rebuilt single EXE.
- Tests: Full `go test ./...` ngoài sandbox và `go vet ./...` pass; UI contract/TypeScript/Vite pass; live backup `flatsome` pass 33,28 giây (`source.tar.gz` 35.710.143 byte, `database.sql` 465.792 byte); cả 4 custom-domain HTTPS trả 200, guest không failed unit; PE subsystem 2 và hidden GUI smoke responsive.
- Security impact: Thêm reverse data flow có scope cố định theo ADR-032, không nới mount/sync. Runtime secret không xuất; arbitrary guest/host path, partial transfer, checksum mismatch, traversal, symlink và special file đều fail closed. Replacement database cần backup và tunnel stopped.
- Artifact: `build/bin/oneclick-dev-server.exe`, 12.485.632 byte, SHA-256 `40C152CB25C105FE1C6E6F0D4015F943A4A60137EF37D8D0EEEEAEC03C7EA7A7`; chỉ còn một EXE.
- Next: User test UI Source/Database/backup; sau phản hồi sẽ triển khai cập nhật source bằng snapshot mới có health-check và rollback.

### 2026-08-23 — README revision 34 — Database source preflight và full live-pass

- Root cause: `wp-config.php` trỏ đúng `127.0.0.1:3306`, nhưng cửa sổ Laragon đang mở không đồng nghĩa MySQL đã chạy. Host không có listener/mysqld nên ba lần `mysqldump` đều dừng ngay với mã 2003/Windows 10061.
- Fixed: Plan probe loopback và nói rõ DB đang chạy/đang tắt. Khi sao chép, backend serialize preflight; nếu tắt chỉ dùng `mysqld.exe` + `my.ini` regular-file cùng bộ dump trong đúng Laragon/XAMPP root, chạy ẩn, chờ tối đa 75 giây rồi mới export. Không có launcher tin cậy thì fail closed với hướng dẫn rõ thay vì lặp raw `mysqldump`.
- Security: Auto-start không dùng command từ frontend/source, không chạy PHP, không đưa password vào argv/env/log và ép classic MySQL bind `127.0.0.1`; MySQL 8 Laragon tắt MySQL X. Source chỉ được đọc; dump/credential tạm được dọn. Không re-import database hoặc đổi tunnel của website đã public trong bài kiểm tra revision này.
- Live evidence: Sau khi Laragon MySQL được bật, history JSON ghi import thành công 12 bảng, 466.564 byte, prefix `wp_`; tiếp đó tunnel/DNS/HTTPS `flatsome.kidgrow.site` thành công. Bài live mới xác minh readiness + dump chỉ đọc + prefix + guest bash syntax trong 9,11 giây và dọn vùng tạm.
- Tests: `go test -p 1 . ./internal/... -count=1`, `go vet . ./internal/...`, `npm run build` và `ONECLICK_LIVE_DATABASE=1 TestLiveAutomaticPlanAndGuestScriptSyntax` pass.
- Artifact: Chờ production build revision 34.
- Next: User mở revision 34, xác nhận website public; lần deploy sau không cần nhớ bật MySQL Laragon thủ công.

### 2026-08-23 — README revision 33 — PHP-FPM native runtime full live-pass

- Root cause 1: PHP-FPM master chạy bằng site user nhưng config dùng `error_log = /proc/self/fd/2`; descriptor thuộc process gọi/không khả dụng trong systemd sandbox nên FPM thoát `78/CONFIG`, socket không được tạo và service restart vô hạn.
- Root cause 2: Sau khi đổi sang log riêng, Nginx vẫn trả 502 vì `RuntimeDirectoryMode=0700` làm ACL mask của named entry `www-data:--x` thành `---`; ACL trên socket đúng nhưng Nginx không traverse được thư mục chứa socket.
- Fixed: Master/worker log dùng hai file site-owned mode 0600 trong private directory. Preflight PHP config chạy bằng chính site user. systemd dùng `RuntimeDirectoryMode=0710`, ACL mask/traverse explicit cho `www-data`, chờ socket tối đa 5 giây trước ACL, xác minh cả directory/socket ACL, reset failed trước retry và giới hạn 3 restart/phút.
- Live evidence: Pipeline thật cho project `4c3c7c2dba7e8f1a` (`flatsome`) đạt `verify -> install -> configure -> start -> verify -> complete 100%` trong 27,55 giây. PHP service active, Nginx HTTP pass, MariaDB có đúng 10 schema privileges/0 cross-schema/0 global, giới hạn 8 connection và 30 giây; state ghi `runtime_ready`.
- Security impact: Không nới IP network, capability, device, namespace, filesystem hay database privilege. Group execute chỉ thuộc private group của site; Nginx chỉ nhận named ACL traverse trên runtime directory và read/write đúng Unix socket. Snapshot gốc không đổi, chưa public Internet.
- Tests: `go test -p 1 . ./internal/... -count=1`, `go vet . ./internal/...`, UI contract/TypeScript/Vite, Wails production build và live full-runtime đều pass. Post-build guest probe xác nhận PHP service, runtime-dir/socket ACL và HTTP nội bộ exit 0. Startup EXE responsive; process smoke đóng sạch, app user đang mở không bị tác động.
- Artifact: Đúng một `build/bin/oneclick-dev-server.exe`, 12.367.872 byte, SHA-256 `F569D4C0078A67A923725C68AD80E5FB960E9B90603F1C114DEBCAF79CA18FA5`, PE GUI subsystem 2.
- Next: Mở revision 33; `flatsome` tiếp tục từ `Runtime sẵn sàng` sang `Sao chép database`.

### 2026-08-23 — README revision 32 — Chi tiết triển khai riêng từng website

- Changed: Chuyển màn Website sang mô hình danh sách → chi tiết. Danh sách chỉ còn website đã lưu, auto-discovery và `Chi tiết/Thiết lập`; toàn bộ chuẩn bị Ubuntu, sao chép source, tạo vùng chạy, database, domain và lịch sử nằm trong trang của đúng website đang chọn.
- UX: Trang chi tiết có nút quay lại dự đoán được, card nhận diện project, progress 5 bước và đúng một CTA tiếp theo theo state JSON. Chỉnh sửa cấu hình từ chi tiết quay lại đúng project sau khi lưu; sidebar Website luôn đưa về danh sách sạch.
- Accessibility: Dùng button semantic, `aria-labelledby`, `aria-current="step"`, nhãn riêng cho nút chi tiết; giữ focus ring, reduced motion, font tối thiểu 12 px và button 36 px theo UI contract.
- Security impact: Chỉ thay đổi điều hướng/trình bày frontend. Mọi mutation vẫn gọi backend bằng `projectPath` của record đang chọn; không đổi VM, source, database, credential, tunnel, policy cách ly hoặc JSON schema.
- Tests: UI Pro Max design/UX/React checks, UI contract, TypeScript/Vite, `go test -p 1 . ./internal/... -count=1`, `go vet . ./internal/...` và Wails production build pass. Final EXE khởi động responsive và đóng sạch trong smoke test; không tạo VM/project hoặc thay đổi source/database.
- Artifact: Đúng một `build/bin/oneclick-dev-server.exe`, 12.366.848 byte, SHA-256 `B52CBF448DD9434309ECEFE6D1C173199726885D307390EC9C8D9FF3C32FC6BB`, PE GUI subsystem 2.
- Next: User mở `Website -> Chi tiết`, xác nhận không còn khối chuẩn bị Ubuntu dưới `Đã tìm thấy trên máy`, rồi tiếp tục live test website đầu tiên trong trang chi tiết.

### 2026-08-23 — README revision 31 — Shared Ubuntu native runtime, ưu tiên cách ly

- Architecture: Thay VM riêng + Docker bằng một fixed Ubuntu 24.04 server `oneclick-server` (2 CPU, 4 GB RAM, 40 GB dynamic disk). Nginx, PHP 8.3, MariaDB và cloudflared binary được cài một lần; project sau chỉ tạo vùng site riêng nên không tải lại Ubuntu/container image.
- Site isolation: Mỗi website có Linux user, source/private/config directory, Unix PHP socket, PHP-FPM systemd service và loopback 127.x origin riêng. Service cấm IP network, capability/device/namespace, dùng strict filesystem/kernel protection, CPU/RAM/PID cap; Nginx chỉ nhận ACL read cần thiết và không đọc `wp-config.php` 0600. Verify bắt buộc thử user chéo trước `runtime_ready`.
- Database isolation: Shared MariaDB chỉ local, tắt local infile/symlink/file import. Mỗi site có schema/user/password riêng; mọi privilege cũ bị revoke, chỉ regrant 10 DDL/DML cần cho WordPress trên đúng schema, tối đa 8 kết nối và 30 giây/statement. Import database giữ bước độc lập, chạy bằng site DB user và xác minh không global/cross-schema privilege.
- Tunnel isolation: Cài một binary official `cloudflared` 2026.8.2 theo URL/SHA-256 pin; mỗi site có connector UID/systemd service/token `LoadCredential` và iptables owner chain riêng. Connector chỉ đi đúng origin, local DNS và Cloudflare TCP 443/7844; chặn host/LAN/private/reserved. Token không ở argv/environment/frontend/state/log.
- Shared-server safety: `Tạo lại từ đầu` bị từ chối nếu bất kỳ project nào có snapshot/runtime/database/tunnel metadata. Copy vẫn một chiều, không host mount. README ghi rõ residual risk của shared guest kernel/Nginx/MariaDB/disk; mô hình này giới hạn compromise ứng dụng thông thường nhưng không được quảng cáo là isolation tuyệt đối như VM-per-site.
- UI: Website dùng wording `Chuẩn bị Ubuntu` và `Tạo vùng chạy riêng`; dialog nêu install-once, không Docker và lớp user/PHP/database riêng. Giữ CRM compact, sidebar fixed/content scroll, font ≥12 px, button 36 px.
- Tests: `go test -p 1 . ./internal/... -count=1`, `go vet . ./internal/...`, runtime/database/tunnel embedded script `bash -n` ngoài sandbox, Wails bindings generation và `npm run build` (UI contract, TypeScript, Vite) pass. Final EXE startup smoke thấy readiness 6/6, 12 website auto-discovery, 2 domain; đóng sạch, `state.json` vẫn schema 3/0 project và `multipass list` vẫn 0 VM.
- Security impact: Siết quyền theo site, fail closed trước public và loại toàn bộ Docker daemon/socket/image surface. Per-site hard disk quota/destroy chưa có; guest live provision/database/tunnel phải pass trước khi đánh dấu production-ready.
- Artifact: Một `build/bin/oneclick-dev-server.exe`, 12.361.728 byte, SHA-256 `6FE01DE5725F5FD4B57BB50BD91DBCB383DCE2D6FDDAC2D0B69ABC0327D5E843`, PE GUI subsystem 2.
- Next: User chạy một WordPress từ baseline sạch qua `Chuẩn bị Ubuntu -> Sao chép website -> Tạo vùng chạy -> Sao chép database` và gửi kết quả; không chuyển website thứ hai trước khi guest isolation/live health của site đầu pass.

### 2026-08-23 — README revision 30 — Sao chép database WordPress độc lập

- Changed: Thêm stage `runtime_ready -> database_importing -> database_ready | database_failed`; domain chỉ mở sau `database_ready`, dừng tunnel trở lại database-ready. UI Website có CTA/modal `Sao chép database`, nguồn `Tự lấy từ WordPress` hoặc file `.sql/.sql.gz`, plan/confirm/progress/result và retry không cài lại runtime.
- Auto export: Parse literal `wp-config.php` tối đa 1 MB mà không execute PHP; chỉ chấp nhận DB host loopback. Tìm dump tool trong đúng Laragon/XAMPP hoặc PATH, dùng temporary `--defaults-extra-file` 0600 trong `data/transfers`, single-transaction và SHA-256; password không qua JS, argument, environment, JSON hay log.
- Guest import: Copy một chiều regular file tối đa 4 GB, verify checksum/gzip/prefix, drop/recreate đúng DB `wordpress` bằng root config nội bộ rồi import bằng user `wordpress`; xác minh table count và `<prefix>options`, cập nhật mutable `wp-config.php`, dọn dump/script/client config. Trước tunnel cập nhật `home/siteurl` theo HTTPS hostname; lỗi rollback Cloudflare và fail closed.
- State: Nâng JSON lên schema 3, migrate schema 1/2, lưu database state/source label/table count/bytes/prefix/time nhưng không lưu DB credential/name/dump path/SQL. Operation database dở được phục hồi `database_failed`.
- UX: Giữ CRM compact, font ≥12 px, button 36 px, focus trap/ring, aria-live progress và reduced-motion; nguồn lỗi tự động luôn chỉ rõ chọn file dự phòng. UI Pro Max được dùng để kiểm tra loading/accessibility/stacking/animation.
- Tests: Database parser literal/escaped/empty local password, remote-host deny, `.sql.gz`/qualified/IF-NOT-EXISTS prefix, option-file escaping, guest policy/secret scope, state lifecycle/interruption/schema migration cùng `go test ./... -count=1`, `go vet ./...`, frontend UI contract/TypeScript/Vite và Wails production build pass. Final EXE mở đúng cửa sổ desktop, readiness 6/6, Website thấy 12 project auto-discovery và đóng sạch sau smoke test. Live guest/full import chưa chạy vì clean baseline có 0 Multipass VM; optional live test dừng rõ ở precondition này và không tạo/mutate VM.
- Security impact: Không chạy source PHP, không quét toàn ổ, không mount host, không bind database port và không cấp root cho SQL dump. File tạm ở application drive được xóa; lỗi/đóng app không cho public website hoặc ảnh hưởng database website khác.
- Artifact: Một `build/bin/oneclick-dev-server.exe`, 12.364.288 byte, SHA-256 `2CC2FFBE84ECBCF882A11E63229C6F42A14EAA132DB9CBAD4602549F0553337B`, PE GUI subsystem 2.
- Next: User tạo/chọn một WordPress, đi tới `Sao chép database`, thử nguồn tự động và xác nhận số bảng. Khi user báo OK mới sang pha tối ưu tốc độ/runtime dùng chung nhưng vẫn giữ VM/database riêng từng website.

### 2026-08-23 — README revision 29 — Menu Domain riêng và chuẩn hóa button

- Changed: Tách quản lý Cloudflare khỏi Cài đặt thành menu `Domain` cấp cao trong sidebar, có số lượng zone, danh sách/trạng thái, kiểm tra lại, thêm/cập nhật token và xác nhận xóa. Trang Cài đặt chỉ còn nơi lưu dữ liệu máy ảo. CTA thiếu domain trong Website điều hướng thẳng sang Domain; modal xuất bản chỉ liệt kê zone đang sẵn sàng và ghép subdomain với zone đã chọn.
- Multi-domain backend: `state.json` nâng lên schema 2 với `domains[]` và `project.domainZone`; loader migrate schema 1. Token vẫn tách riêng trong native credential vault theo zone. Backend chọn explicit zone hoặc longest hostname suffix, chặn zone không khớp, chặn xóa domain đang được project dùng và dùng lại đúng zone cho stop/recovery.
- UI consistency: Tất cả action button primary/secondary/tertiary và icon button cao 36 px, cùng padding/radius/font; tertiary không còn tự giảm còn 32 px. UI contract trong `npm run check:ui` tự fail khi font dưới 12 px, action/icon button sai 36 px hoặc thiếu focus-visible/reduced-motion.
- Tests: `npm run build` pass gồm UI contract, TypeScript và Vite; `go test -p 1 ./... -count=1` và `go vet ./...` pass; UX validation kiểm tra loading/stacking/continuous animation không phát hiện yêu cầu code mới. Wails production build dùng verified `frontend/dist` và pass. Live Windows visual smoke mở đúng EXE, thấy menu Domain với `gaumobile.com` + `kidgrow.site`, chuyển sang Cài đặt chỉ còn storage card; sau kiểm tra cửa sổ được đóng và không còn process chạy.
- Security impact: Không đưa token vào JSON/frontend response/log; xóa zone có usage guard; không đổi VM/snapshot/runtime/firewall/tunnel credential policy. Giao diện không tự tạo DNS hay public website khi chỉ thêm domain.
- Artifact: Một `build/bin/oneclick-dev-server.exe`, 12.262.400 byte, SHA-256 `B201F4738498CA051B624833E67D366EEEA686F2AC663F4307B94AFB7E0FFFE4`, PE GUI subsystem 2.
- Next: User mở revision 29, kiểm tra menu Domain riêng và độ cao button ở các màn; sau khi xác nhận mới tiếp tục pha kế tiếp.

### 2026-08-23 — README revision 28 — Chuẩn hóa CRM và cố định sidebar

- Changed: Chuẩn hóa toàn bộ UI về thang chữ 12/13/14/17/22, bỏ toàn bộ font 9–11 px, tăng độ rõ của table header/status/helper text, giữ palette blue/slate truyền thống và density CRM. `html/body/#root` khóa outer scroll; `.app-shell` theo viewport, sidebar giữ nguyên và chỉ `.main-content` cuộn dọc/chặn tràn ngang.
- Accessibility: Thêm focus trap/restore dùng chung cho năm dialog, `Escape` chỉ đóng khi operation không chạy; giữ semantic dialog/aria-live, focus-visible và reduced-motion. Thêm `frontend/scripts/check-ui.mjs`; `npm run build` fail nếu có font dưới 12 px hoặc thiếu focus/reduced-motion contract.
- Build reliability: Thêm nested `data/go.mod` và ignore `data/multipass/`. Ranh giới này sửa dứt điểm `go list all`/Wails bindings thất bại vì ACL Multipass, không di chuyển hoặc đọc nội dung VM.
- Live visual evidence: Bản EXE kiểm tra hiển thị sạch ở `Kiểm tra máy`, `Website` và `Cài đặt`. Trên trang Cài đặt dài, sau scroll content từ đầu xuống form Cloudflare, logo/menu/footer sidebar vẫn giữ nguyên vị trí; scrollbar chỉ nằm ở cột content. Typography/badge/form không còn chữ dưới 12 px.
- Tests: UI Pro Max design-system + React/UX validation; `npm run build` pass gồm UI contract, TypeScript và Vite; `go list all`, `go test -p 1 ./... -count=1`, `go vet ./...` pass; Wails bindings generation pass sau module boundary và production EXE build pass từ verified `dist`. Lần frontend build lặp trong Wails bị Windows trả `esbuild spawn EPERM`; direct frontend build đã pass và final Wails dùng `-s -skipbindings` để embed đúng bundle đã xác minh, không che giấu lỗi compile.
- Security impact: Không đổi backend, dependency, VM, snapshot, database, token hoặc tunnel policy. Kho Multipass vẫn `D:\oneclick-dev-server\data\multipass`, service Running, 0 VM. Focus/scroll chỉ thay đổi UI; nested module không cấp thêm ACL.
- Artifact: Một `build/bin/oneclick-dev-server.exe`, 12.237.824 byte, SHA-256 `2F7B865A954762771351B1A4A05AB6F25E0E79401859E503EDBBC01552BC9DFD`, PE GUI subsystem 2.
- Next: User chạy revision 28 và phản hồi cảm nhận UI/sidebar. Nếu đạt, pha kế tiếp là danh sách nhiều domain và chọn domain cho từng website.

### 2026-08-23 — README revision 27 — Sửa helper UAC và live-pass kho Multipass ổ D

- Root cause: Revision 26 truyền target/result sang elevated helper bằng process environment. Sau UAC, Windows không bảo đảm child có các biến đó, nên helper thoát mã 1 trước khi ghi kết quả và UI chỉ nhận `helper kết thúc nhưng không trả về kết quả`.
- Changed: Parent tạo `request.json` schema cố định trong `%LOCALAPPDATA%\OneClickDevServer\operations\storage-*`, launcher chỉ nhúng literal đường dẫn request và helper EXE, elevated child tự xác thực request/path rồi ghi `result.json` theo từng stage. Journal được giữ lại; lỗi UI kèm exact log path. Unknown field/command/script, reparse path và request sai operation đều fail closed.
- Live evidence: User xác nhận UI báo thành công. Latest journal `storage-2053155453/result.json` có `success=true`, `stage=complete`; machine registry là `MULTIPASS_STORAGE=D:\oneclick-dev-server\data\multipass`, dịch vụ `multipass` Running. Smoke VM `oneclick-storage-smoke` boot Ubuntu 24.04.4, disk 5 GB, `multipass exec` trả `storage-smoke-ok`; sau đó exact delete/purge thành công và `multipass list --format json` trở lại rỗng. Source `D:\laragon\www\flatsome` và `D:\laragon\www\noibo.kientrucnhacuagio` vẫn tồn tại.
- Tests: `go test -p 1 . ./internal/... -count=1` và `go vet . ./internal/...` pass với workspace `GOCACHE`; `npm run build` pass. Không dùng `go test ./...` sau khi kho tồn tại vì wildcard cố đọc `data\multipass` có ACL hệ thống; explicit root + `internal/...` là toàn bộ Go packages của source. Production EXE hiện tại là build đã pass trước live test.
- Security impact: Không nới quyền hoặc command surface. UAC vẫn do người dùng xác nhận; PowerShell chỉ launch fixed helper hidden. Request/result journal giới hạn schema/kích thước, không nhận payload từ frontend, rollback registry/service vẫn giữ nguyên. Smoke test chỉ tạo và purge đúng VM do phiên kiểm thử sở hữu.
- Artifact: Một `build/bin/oneclick-dev-server.exe`, 12.235.776 byte, SHA-256 `06E79CBE68156B34660B9D03E9E8E74BEB9FEF5CDB15C617D76B0F12389A0CDF`, PE GUI subsystem 2.
- Next: Chờ người dùng xác nhận sang phần tiếp theo; không gộp thêm thay đổi vào pha storage đã live-pass.

### 2026-08-23 — README revision 26 — Chọn nơi lưu dữ liệu trước deployment đầu tiên

- Changed: Thêm card `Nơi lưu dữ liệu máy ảo` tại Cài đặt, tự đề xuất `D:\oneclick-dev-server\data\multipass`, hiển thị current path/free space/project/VM count; chọn path chỉ tạo plan và có xác nhận thứ hai trước UAC. Toàn text màn Cài đặt được chặn tối thiểu 12 px.
- Backend/security: Thêm `internal/datastore/`; dùng registry `MULTIPASS_STORAGE` theo cơ chế Canonical, chỉ khi 0 project + 0 VM. Target phải fixed local drive, empty, non-reparse, ≥10 GB và tách default/system tree. Chính EXE chạy fixed elevated helper mode, PowerShell chỉ là UAC launcher ngắn/hidden; baseline copy khi service dừng, postflight danh sách sạch và rollback registry/service khi lỗi. Frontend không truyền command, helper không xóa source/state/token/default store và không hot-migrate.
- Evidence: Trước build, máy test có Multipass/multipassd 1.16.3, driver `virtualbox`, service Running, `multipass list` rỗng và chưa có machine `MULTIPASS_STORAGE`. Mutation chưa tự chạy; chờ user bấm xác nhận để live-pass đúng pha.
- Tests: `go test -p 1 ./... -count=1`, `go vet ./...`, datastore target/result/baseline/launcher tests, Wails binding generation, TypeScript/React/Vite production build và UI UX loading/accessibility/reduced-motion review pass. Chạy tuần tự để tránh antivirus giữ tạm test EXE khi Go dọn nhiều package song song.
- Artifact: Một `build/bin/oneclick-dev-server.exe`, 12.220.928 byte, SHA-256 `CCACB0756ADC3ECFA887862A7F7F9BA0C3CF56FC63319B69C986207550766866`, PE GUI subsystem 2.
- Next: User mở revision 26, vào `Cài đặt`, dùng thư mục đề xuất và xác nhận UAC; báo kết quả đường dẫn/status. Không triển khai phần tiếp theo trước phản hồi này.

### 2026-08-23 — README revision 25 — Baseline sạch và tự nhận website

- Changed: Bỏ hoàn toàn tính năng/UI/binding/package `Chuyển dữ liệu máy ảo`. Thêm detector chỉ đọc immediate child của `laragon\www`, `xampp\htdocs`, `wamp64\www` trên fixed drive (và ba conventional root Linux), giới hạn 128, deduplicate/sort; màn Website tự hiện `Chưa thiết lập` nhưng chỉ lưu JSON sau xác nhận.
- Cleanup live: Xóa owned DNS/tunnel Cloudflare của `flatsome`; purge VM `flatsome`; bỏ saved-state VirtualBox hỏng rồi purge VM `noibo`; xóa đúng hai record JSON. `multipass list` hiện rỗng, state có 0 project, `kidgrow.site` connection vẫn giữ và hai source folder ở `D:\laragon\www` vẫn tồn tại.
- Reliability/security: Tunnel ghi guest phase journal, sau context deadline sẽ probe recovery trước khi kết luận lỗi và chỉ HUP proxy khi cấu hình đổi. `DeleteDeployment` có thể tìm lại resource theo exact managed comment/tunnel name khi state thiếu ID; không xóa DNS/tunnel unrelated. JSON removal chỉ nhận exact canonical paths và không chạm source. Auto-discovery không recurse, không chạy code và không tự tạo deployment.
- Live evidence: Detector nhận 12 WordPress immediate-child trong Laragon, gồm đúng `flatsome` và `noibo.kientrucnhacuagio`. Unit test discovery immediate-only, exact state removal và owned-resource cleanup pass. `go test ./...`, `go vet ./...`, TypeScript/Vite, UI UX loading/accessibility/stacking review và Wails production build pass.
- Artifact: Một `build/bin/oneclick-dev-server.exe`, 12.152.832 byte, SHA-256 `11AA5F98E345ABC734E556F2422E2C9D2DDE141E25A0A9C751DD773418B501A1`, PE GUI subsystem 2.
- Next: Mở revision 25, vào `Website`, chọn một mục `Chưa thiết lập` và chạy lại một dự án từ đầu; sau khi pass mới tối ưu cache/speed/security từng phase.

### 2026-08-23 — README revision 24 — Không bị kẹt bởi cache DNS Windows

- Root cause: `kidgrow.site` đã active trong Cloudflare API; Cloudflare `1.1.1.1`, Google `8.8.8.8` và Quad9 `9.9.9.9` đều trả `lynn.ns.cloudflare.com` + `tricia.ns.cloudflare.com`, nhưng Windows default resolver vẫn cache bốn NS ZoneDNS nên GUI báo chờ sai quá 30 phút.
- Changed: Nameserver lookup chạy song song system resolver và ba public recursive resolver, giới hạn 6 giây. Hai public resolver độc lập đồng thuận sẽ là authority cho website công khai; system DNS vẫn là fallback khi public chưa đồng thuận hoặc bị chặn.
- Tests: Thêm unit test public-consensus thắng stale Windows cache và fallback an toàn. Live Go DNS test trả đúng Cloudflare NS trong 0,07 giây; `go test ./...`, `go vet ./...`, Wails production build pass.
- Security impact: Không bỏ Cloudflare API `zone active` gate và không dựa vào một resolver đơn lẻ. Không mở port, không đổi token/tunnel/runtime policy.
- Artifact: Một `build/bin/oneclick-dev-server.exe`, 12.133.376 byte, SHA-256 `3EF766EE89163838A0323C6987FA31DA0B00F4BB615E8446CD76F3ACB7833FCC`, PE GUI subsystem 2.
- Next: Mở revision 24, bấm `Kiểm tra lại`, sau đó chọn subdomain và chạy external Named Tunnel live test.

### 2026-08-23 — README revision 23 — Domain riêng bằng Cloudflare Named Tunnel

- Changed: Thay Quick Tunnel bằng luồng domain ổn định. Màn `Cài đặt` xác minh zone/nameserver, lưu scoped Cloudflare API token trong native OS vault; Website cho chọn subdomain, xem plan, xuất bản/dừng và khôi phục đúng bước từ JSON.
- Backend: Thêm Cloudflare API client và transactional Named Tunnel/CNAME lifecycle. DNS record không thuộc project bị từ chối ghi đè; connector token không trả về frontend/không vào state/log, chỉ chuyển bằng file tạm rồi Compose secret trong guest. Stop ưu tiên dừng connector và vẫn giữ đủ ID để retry cleanup nếu token/provider lỗi.
- Guest security: `cloudflared` digest-pinned, UID `65532`, read-only, cap-drop/no-new-privileges/resource cap, không port/socket; Nginx nối ingress internal riêng, PHP/DB không nối egress. Egress firewall chặn host/LAN/private/reserved ranges trước khi connector start. WordPress/Nginx nhận HTTPS proxy header an toàn.
- Reliability: OpenGuest cho phép Multipass/VirtualBox warm-up 90 giây và retry ownership marker tối đa 55 giây, tránh false failure do SSH handshake Windows chậm. External Python health-check dùng `HTTPSHandler` đúng API và chỉ cho redirect cùng HTTPS hostname.
- Tests: `go test ./...`, `go vet ./...`, TypeScript/Vite và Wails production build pass. Unit test hostname/DNS conflict/rollback/state/tunnel policy pass. Live guest Bash syntax và prepare → Compose config/firewall/secret ownership → rollback pass; sau rollback không còn tunnel network, ba runtime container vẫn healthy. External Cloudflare/HTTPS chưa chạy vì `kidgrow.site` hiện còn ở ZoneDNS.
- Files: Thêm `internal/cloudflare/`, `internal/tunnel/`, `internal/secrets/`; mở rộng `app.go`, `internal/state/`, `internal/runtime/`, `internal/vm/guest.go`, Wails bindings và `frontend/src/App.tsx/App.css`.
- Artifact: Một `build/bin/oneclick-dev-server.exe`, 12.123.648 byte, SHA-256 `3C2AABF073A94C9472BABCF449C6C82E41A8CB51B87AC0D261FED67F0EE7B32D`, PE GUI subsystem 2.
- Security impact: Không mở port/host mount/Docker socket và không nới quyền website. Tunnel egress được tách riêng; fail closed nếu DNS, firewall, connector policy hoặc HTTPS health-check không đạt.
- Next: Thêm `kidgrow.site` vào Cloudflare, đổi nameserver tại ZoneDNS, nhập token ở `Cài đặt`, chọn `flatsome.kidgrow.site` (hoặc subdomain khác) và chạy external live test.

### 2026-08-23 — README revision 22 — WordPress runtime full live-pass

- Root cause 1: Compose dùng flow sequence `tmpfs: [/tmp:rw,noexec,nosuid,nodev,size=64m]`; YAML tách dấu phẩy thành nhiều mount và Docker coi `nodev` là path. Đổi PHP/Nginx sang block list; unit test cấm mọi `tmpfs: [`.
- Root cause 2: Compose file secret là bind mount giữ permission source. PHP container chạy `33:33` không đọc được file `0400 root`, khiến WordPress HTTP 500 và Nginx không healthy. Giữ DB root secret `0400 root:root`; đổi DB password/WordPress keys thành `0440 root:www-data`, vẫn chỉ trong guest.
- Live recovery: Dừng lượt health chờ lỗi, restart mềm đúng VM sau SSH channel nhiễu, không purge. VM boot lại với 2 CPU/2 GB, Docker khôi phục container. Full backend pipeline `verify → install → configure → pull → start → verify` pass trong 274,77 giây và atomically persist `runtime_ready`.
- Live evidence: `nginx`, `php`, `db` đều healthy; internal HTTP pass; hai network `internal=true`; PortBindings rỗng; read-only/no-new-privileges/cap-drop/PID/memory/user/mount policy inspect pass. Secret permissions đúng; snapshot `3c2684fd24d4a40a` và source host không đổi; website chưa public.
- Tests: `go test ./...`, `go vet ./...`, React/TypeScript/Vite, live syntax, live Docker phase và full live runtime/state pass. Wails production build pass. Final EXE 11.839.488 byte, SHA-256 `B90326DA198122D71FFDCAD2B616AAA2E80A66009F82198590161685EB0DF63C`, PE GUI subsystem 2; `build/bin` có đúng một file.
- Security impact: Chỉ cấp group read cho đúng UID/GID web trong dedicated guest; không nới network/capability/mount/port. Fail-closed health gate đã chứng minh website không sang `runtime_ready` trước khi HTTP/policy pass.
- Next: User mở revision 22 để xác nhận UI `Runtime sẵn sàng`; sau đó triển khai Quick Tunnel và database export/import có consent.

### 2026-08-23 — README revision 21 — Chặn treo phase Docker ở 32%

- Root cause evidence: Khi GUI vẫn pulse ở 32% hơn 6 phút, trong guest không còn `apt-get`, `dpkg`, `systemctl` hay installer process; ba package đã đúng exact version, Docker/containerd active và daemon config revision 20 đã ghi đủ. Lệnh guest đã hoàn tất nhưng blocking service restart/Multipass exec channel không đóng, giữ frontend chờ giả. Đóng app không làm mất package, image cache, snapshot hoặc secret.
- Changed: Daemon config ghi qua temp + `cmp`; không đổi thì không restart. Khi cần, dùng `systemctl --no-block restart`, poll Docker/Compose tối đa 120 giây. Thêm timeout riêng cho cả năm phase để context hủy CLI khi kênh lỗi.
- Live evidence: Sau khi giải phóng orphan Multipass CLI, guest syntax pass 10,13 giây; test chạy thật idempotent phase `install` trên VM `flatsome` pass 12,46 giây. Docker 29.1.3, Compose 2.40.3, containerd 2.2.1 và MariaDB digest cache giữ nguyên.
- Tests: `go test ./...`, `go vet ./...`, live syntax và live Docker phase pass; Wails production build pass. Final EXE 11.839.488 byte, SHA-256 `A3269865B0F204429BACD1EDF7669F283F932DDDF7D256515B3BA1CA391C9DB9`, PE GUI subsystem 2; `build/bin` có đúng một file.
- Security impact: Không đổi permission/network/digest policy; timeout chỉ fail closed. Source/snapshot không bị sửa, chưa có container website chạy và chưa public.
- Next: User mở revision 21, app tự phục hồi `runtime_installing` thành `runtime_failed`, bấm thử lại và theo dõi phase chuyển qua 34% trong khoảng vài chục giây.

### 2026-08-22 — README revision 20 — Runtime image pull retry và diagnostic

- Root cause evidence: Lượt cài đầu đã cài Docker `29.1.3`, daemon active và tải hoàn chỉnh MariaDB digest `67873d…` (463 MB). VM còn 5,7 GB; DNS/registry hoạt động; manifest WordPress và Nginx cùng trả thành công. WordPress image chưa xuất hiện, nên lượt pull kế tiếp đã bị gián đoạn; exact low-level cause bị revision 19 cắt mất vì state giữ 1.200 ký tự đầu của log layer MariaDB.
- Changed: Docker giảm `max-concurrent-downloads` xuống 1, tăng download attempt lên 5; từng image dùng quiet pull, timeout 10 phút, bốn lượt retry/backoff và cache resume. Runtime error loại layer noise, giữ 20 dòng cuối; progress hiển thị thời gian đã chờ sau 20 giây.
- Tests: `go test ./...` pass; test mới bắt buộc retry config và giữ lỗi cuối; guest installer `bash -n` live-pass lại trên VM `flatsome`; WordPress/Nginx manifest probe live-pass. Wails production build pass. Final EXE 11.838.976 byte, SHA-256 `8CA2C0BC990AFAB980B59AB13456C440EB235843AF28CC357D99C131ECEFEA7D`, PE GUI subsystem 2; `build/bin` có đúng một file.
- Security impact: Không đổi digest lock, isolation hoặc network policy. Pull chỉ tới cùng official digest đã pin; source/snapshot không bị sửa, MariaDB layer cache được giữ, chưa có container chạy và chưa public.
- Next: User mở revision 20 và bấm thử lại; installer reuse Docker/MariaDB cache rồi tiếp tục WordPress/Nginx, start và health/policy check.

### 2026-08-22 — README revision 19 — WordPress guest runtime built

- Changed: Thêm state/history `runtime_installing/runtime_failed/runtime_ready`, backend-only runtime identity, embedded package/image lock, verified guest handle, phased Docker/Compose installer, guest-only secret generation, mutable working copy tách snapshot, WordPress PHP 8.3 FPM + Nginx + MariaDB trắng và policy/health inspection trước khi cho sang bước tunnel.
- UI/UX: CTA `Cài môi trường`, plan ngắn gọn, progress pulse cho apt/image/health, result/retry và trạng thái CRM `Runtime sẵn sàng`; áp dụng loading-state/accessibility/stacking guidance, không animation trang trí hoặc terminal popup.
- Files: Thêm `internal/runtime/`; mở rộng `internal/vm/guest.go`, `internal/state/`, `app.go`, Wails bindings và `frontend/src/App.tsx/App.css`.
- Tests: `go test ./...`, `go vet ./...`, React/TypeScript/Vite và Wails production build pass; runtime lock/digest/forbidden-policy/state interruption tests pass; guest installer `bash -n` live-pass trên dedicated VM `flatsome`. Final EXE 11.835.392 byte, SHA-256 `D2C7205B7752929F07B384E6E9F00068F8AF9D55EA68998D9093A01EA003BB20`, PE GUI subsystem 2; `build/bin` có đúng một file.
- Security impact: Website chỉ chạy sau khi container policy và health pass; không host mount, Docker socket, published port hoặc outbound network; DB/WordPress secret chỉ tồn tại trong guest. Chưa chạy runtime thật và chưa public trong phiên code này.
- Next: User mở revision 19, chọn `flatsome`, bấm `Cài môi trường`, chờ ba container healthy và gửi kết quả UI/lỗi; sau live-pass triển khai Quick Tunnel.

### 2026-08-22 — README revision 18 — Snapshot/copy một chiều live-pass

- Changed: Thêm copy-plan GUI, progress/result/retry, deterministic snapshot builder, denylist/limits/reparse guard, per-file manifest, archive checksum, verified Multipass transfer, immutable guest commit/current link và state/history `copying/copy_failed/source_ready`; thay đổi cấu hình sẽ invalidate snapshot metadata và yêu cầu copy lại.
- UI/UX: Giữ CRM compact theo design-system query; một primary CTA, text ngắn, disabled/loading rõ, technical detail thu gọn, không emoji/terminal popup.
- Live evidence: `D:\laragon\www\flatsome` tạo snapshot `3c2684fd24d4a40a`, 5.510 file, 99.812.260 byte; Ubuntu guest verify checksum, readonly commit và current link pass. Lần chạy lại cùng source giữ đúng ID và pass trong 15,10 giây. Snapshot thử nghiệm không deterministic `9563a8b2524aa533` đã được xoá sau khi xác minh current trỏ bản mới; source host và VM `noibo` không bị sửa.
- Tests: `go test ./... -count=1`, `go vet ./...`, targeted live `TestLiveSnapshotTransfer`, React/TypeScript/Vite và Wails production build pass. Final EXE 11.776.000 byte, SHA-256 `31955CBECE8C56B8F56D581457233B28AF5A1461CA86BB681A5EE6313DBF045A`, PE GUI subsystem 2; `build/bin` có đúng một file.
- Security impact: Website chỉ được đọc/đóng gói/chuyển một chiều; `.env`, WordPress config, credential/key/cert/log/cache/DB dump bị loại; không mount host, không execute source, chưa mở tunnel.
- Next: User test CTA `Sao chép website`; sau phản hồi, cài guest runtime/container/firewall và chỉ chạy source khi các policy đó pass.

### 2026-08-22 — README revision 17 — Clean VirtualBox và VM lifecycle live-pass

- Root cause final: `VBoxHardening.log` xác nhận `RTR3InitEx rc=-1912` = support-driver version mismatch. Windows uninstall registry đồng thời giữ hai MSI `Oracle VirtualBox 7.2.16` và `7.1.18`; install directory cũng còn file 7.2.16. Kaspersky DLL bị hardening sàng lọc nhưng không phải nguyên nhân dừng VM sau clean install.
- System repair: Sao lưu ba file metadata Multipass/VirtualBox, gỡ sạch cả hai MSI qua product code, giữ install directory cũ làm recovery, cài lại gói Oracle 7.1.18 đã pass exact SHA-256 và Authenticode. Sau live validation, stale program backup và temp installer đã xóa; registry chỉ còn một MSI 7.1.18, không còn file 7.2.16/pending replacement.
- Product hardening: Backend kiểm tra exact single Oracle MSI registration ngoài CLI/driver/pending-reboot. Installer mới clean-replace mọi Oracle VirtualBox MSI conflict trong một UAC, giữ recovery directory khi thất bại và chỉ xóa sau install pass; script được parse bằng Windows PowerShell trong unit test.
- VM evidence: Fresh `oneclick-flatsome-4c3c7c2dba` vượt hardening và đạt SSH; marker `owner=oneclick image=24.04`, Ubuntu 24.04 x86_64, 2 CPU, mounts rỗng. Reuse health-check pass 8,38 giây; graceful stop/start rồi backend reuse pass 102,69 giây. VM hiện `Running`; `noibo` vẫn `Suspended`; source chưa mount/copy/chạy/public.
- Timeout/cloud-init: Fresh boot hoàn tất cloud-init ở 2 phút 25 giây nên false-failure 2 phút được nâng thành 5 phút. Bỏ trường `permissions` bị Multipass serialize thành numeric `420`; cloud-init dùng default safe `0644` cho marker ở VM mới.
- Tests/artifact: `go test ./...`, `go vet ./...`, `npm run build`, Wails production build pass; live clean-backend/VM/lifecycle pass. [build/bin/OneClick-Dev-Server.exe](build/bin/OneClick-Dev-Server.exe), SHA-256 `803DB92CC2D6465FD6FD994AF052BEDA4B088BF1D746A7B7F6CB2D8D4F125FD5`, PE GUI subsystem `2`; `build/bin` chỉ có một file.
- Next: User mở revision 17 và bấm `Tiếp tục` cho `flatsome`; bước environment sẽ reuse VM đã pass. Tiếp theo hiện thực immutable snapshot/copy manifest và transfer vào guest trước khi cài runtime.

### 2026-08-22 — README revision 16 — Chặn mixed VirtualBox driver trước khi tạo VM

- Root cause confirmed: Windows `LastBootUpTime` là 16:25, sớm hơn lần cài/launch lúc 22:05; máy chưa restart sau install. `VBoxManage.exe` là `7.1.18r173720`, nhưng `VBoxSup.sys` và `VBoxUSBMon.sys` trên host còn nhánh `7.2.16`; registry xác nhận hai driver đang chờ Windows thay thế. Cấu hình trộn version làm `VBoxHardening` trả `E_FAIL` trước khi Ubuntu chạy.
- Changed: Windows backend đọc version resource của bốn VirtualBox driver và kiểm tra pending file replacement. Readiness hiển thị `Cần khởi động lại Windows`; VM plan/create trả `RebootRequired` và dừng trước `multipass launch`, nên mở lại app khi chưa restart cũng không còn tạo-xóa VM lặp.
- Tests: Live read-only test trên máy thật pass, xác nhận backend thấy driver 7.2.16, `Ready=false`, `RebootRequired=true` và không sinh install action. `go test ./...`, `go vet ./...`, `npm run build`, Wails production build đều pass; PE subsystem là Windows GUI (`2`).
- VM safety: Failed `oneclick-flatsome-4c3c7c2dba` không còn tồn tại; `oneclick-noibo-kientrucnhacuagio-6be4db4547` vẫn Suspended. Source website chưa mount/copy/chạy/public.
- Artifact: [build/bin/OneClick-Dev-Server.exe](build/bin/OneClick-Dev-Server.exe), SHA-256 `BA7F2FD646BC61334C2B43E58BD13718D8CEA5A9CB7B8EC61A68FA67AD2BA8B4`; `build/bin` chỉ có một EXE.
- Next: User chọn **Restart** trong Windows (không chỉ đóng/mở ứng dụng). Sau boot, xác nhận toàn bộ VBox driver là 7.1.18 rồi chạy fresh VM integration test cho `flatsome`.

### 2026-08-22 — README revision 15 — Cài thành công VirtualBox 7.1.18, chờ restart

- Live install: Chạy lại installer sau khi user có mặt; official URL, SHA-256 pin, Oracle Authenticode, UAC và silent install đều pass. `VBoxManage --version` hiện trả `7.1.18r173720`.
- Reboot boundary: Fresh VM được thử ngay sau khi cài để xác định trạng thái và dừng sớm ở `VBoxHardening.log`; đây là driver/hardening cũ chưa được Windows nạp lại. Không lặp recreate trước reboot và không coi đây là lỗi SSH/website.
- VM safety: Exact VM test `oneclick-flatsome-4c3c7c2dba` đã được purge sau lần pre-reboot thất bại. VM `oneclick-noibo-kientrucnhacuagio-6be4db4547` vẫn Suspended, không bị sửa. Source `D:\laragon\www\flatsome` chưa mount/copy/chạy/public.
- Tests: Sau live install, `go test ./...` và `go vet ./...` đều pass; `VBoxManage --version` và `multipass list --format json` xác nhận đúng hypervisor/VM state.
- Artifact: Code và EXE không đổi sau revision 14; `build/bin/OneClick-Dev-Server.exe`, SHA-256 `4B3D96F1AC63CECE241313BE66C6D902180F7F6F449E8AB976787347806900B3`; chỉ còn một EXE.
- Next: Restart Windows một lần. Sau khi máy lên lại, chạy fresh VM integration test cho `flatsome`; chỉ chuyển bước provisioning khi EFI, SSH, ownership, Ubuntu/architecture, no-mount và cloud-init `boot-finished` đều pass.

### 2026-08-22 — README revision 14 — Pin VirtualBox 7.1.18, loại BIOS workaround

- Root cause refinement: VirtualBox 7.2.16 lặp EFI `#GP` trước guest OS. BIOS cho Ubuntu boot/SSH một lần nhưng sau power cycle fresh/reused VM đều kẹt ở initramfs `Loading essential drivers`; vì vậy revision 13 firmware auto-repair bị loại bỏ, không tuyên bố pass giả.
- Changed: Exact VirtualBox 7.1.18 được pin từ Oracle official URL với SHA-256 `de14e4d6572e5a602e5053f3fd8c641356fd377ef9baa813136ae32711d19488`; backend chỉ Ready với version prefix `7.1.18r`, version khác nhận CTA cài. Authenticode PowerShell cô lập module path; gói 7.1.18 đã pass URL/hash/Oracle signature và tới UAC, nhưng user chọn No nên chưa thay đổi host.
- VM safety: Exact VM `flatsome` lỗi đã stop/purge; `oneclick-noibo-kientrucnhacuagio-6be4db4547` vẫn Suspended và không bị sửa. Source `D:\laragon\www\flatsome` chưa mount/copy/chạy/public. Host hiện vẫn là VirtualBox `7.2.16r174877`, nên create bị chặn đúng thiết kế.
- Health: Cloud-init cố định `Etc/UTC`, tắt background package update/upgrade ở bước environment và fresh VM phải có `boot-finished`; parser mounts whitespace vẫn fail-open chỉ cho empty object/list và fail-closed cho malformed/non-empty. SSH tiếp tục là connectivity authority khi IPv4 host là `N/A`.
- Tests: `go test ./...`, `go vet ./...`, `npm run build`, Wails production build pass; version pin/signature path/cloud-init/mount guard có unit test; PE subsystem Windows GUI (`2`). Live 7.1.18 VM retest đang chờ install + restart.
- Artifact: `build/bin/OneClick-Dev-Server.exe`, SHA-256 `4B3D96F1AC63CECE241313BE66C6D902180F7F6F449E8AB976787347806900B3`; chỉ còn một EXE.
- Next: Mở revision 14, vào `Kiểm tra máy`, bấm cài VirtualBox 7.1.18, chọn Yes ở UAC và restart; sau đó tạo lại `flatsome`.

### 2026-08-22 — README revision 13 — Sửa dứt điểm VirtualBox EFI và nhận diện SSH

- Root cause: Serial console của exact VM `oneclick-flatsome-4c3c7c2dba` ghi `X64 Exception Type - 0D (#GP)` trong `VBoxEfiFirmware/UefiCpuPkg/CpuDxe` trước khi Ubuntu chạy. Đây là crash firmware EFI VirtualBox 7.2.16, không phải lỗi source website, cloud-init hay thiếu restart.
- Live recovery evidence: Đã stop/purge đúng VM `flatsome`, tạo lại cùng fixed config, xác nhận EFI crash lặp lại, sau đó đổi riêng VM sang BIOS. Ubuntu 24.04.4 boot thành công; guest có `enp0s3 10.0.2.15/24`, default route NAT, `multipass exec`/SSH hoạt động, marker `owner=oneclick image=24.04`, `x86_64`, 2 CPU và không host mount. VM `noibo` và source `D:\laragon\www\flatsome` không bị sửa/xoá/copy/chạy/public.
- Changed: Launch poll thêm SSH probe nên `N/A` không còn bị kết luận sai. Fresh Windows VirtualBox VM không SSH sau 30 giây được stop, hidden UAC sửa SYSTEM VirtualBox registry sang BIOS với pre/post verification, rồi start lại qua Multipass. Parser `mounts` đọc JSON theo cấu trúc nên object/array rỗng có whitespace không bị báo nhầm; malformed/non-empty vẫn fail closed. Existing VM không bị tự đổi firmware; confirmed recreate guard vẫn giữ nguyên.
- Files: `internal/multipass/vbox_windows.go`, `internal/multipass/vbox_other.go`, `internal/multipass/vbox_windows_test.go`, `internal/vm/environment.go`, `internal/vm/environment_test.go`, frontend dist, `README.md`, rebuilt single EXE.
- Tests: `TestLiveExistingEnvironment` chạy với `D:\laragon\www\flatsome` PASS qua toàn bộ backend create/reuse; live boot/network/SSH/marker/OS/arch/info evidence; unit test mounts whitespace/malformed; `go test ./...`, `go vet ./...`, `npm run build`, Wails production build pass; PE subsystem Windows GUI (`2`).
- Security impact: Firmware repair chỉ nhận exact deterministic `oneclick-*` name, yêu cầu VM poweroff, chỉ áp dụng fresh VM vừa được app tạo và không xoá instance/source. Elevated payload pin VBoxManage/name/result bằng PowerShell literals; terminal ẩn nhưng UAC vẫn rõ ràng.
- Artifact: `build/bin/OneClick-Dev-Server.exe`, SHA-256 `04A76F526C937DBA310A5684461C142D166BAFAD56507CE683803986907901CA`; chỉ còn một EXE.
- Next: User mở revision 13 và bấm `Tiếp tục` trên project `flatsome`; bước VM sẽ hoàn tất từ SSH hiện có, sau đó code snapshot/copy website vào guest.

### 2026-08-22 — README revision 12 — Phục hồi VM Running nhưng mất IP/SSH

- Changed: Existing VM `Running` với IP rỗng/`N/A` được probe SSH tối đa 12 giây; nếu không phản hồi, UI báo mất kết nối và hiện `Tạo lại từ đầu` thay vì chờ 90 giây rồi đưa raw SSH error.
- Recovery guard: Sau confirmation riêng, backend đọc lại đúng deterministic VM đã ghi trong project history. VM `Running` chỉ được stop-force/delete-purge khi đồng thời không IP usable và một SSH probe mới thất bại; VM có IP hoặc SSH hoạt động bị từ chối xoá.
- Evidence: Windows đã restart lúc khoảng 16:25. Sau restart, `oneclick-flatsome-4c3c7c2dba` vẫn `Running`, IPv4 `N/A`; Multipass Event Log và `info` đều ghi `Timeout connecting to 127.0.0.1`. Đây là VM cũ hỏng, không còn là pending reboot.
- Files: `internal/vm/environment.go`, `internal/vm/environment_test.go`, `frontend/src/App.tsx`, frontend dist, `README.md`, rebuilt single EXE.
- Tests: Unit test guard Running/IP/SSH; `go test ./...`, `go vet ./...`, `npm run build` và Wails production build pass; PE subsystem Windows GUI (`2`).
- Security impact: Nới recovery có kiểm soát so với ADR-015 nhưng vẫn không nhận VM name từ frontend, bắt buộc state record + deterministic name + explicit confirmation + no-IP + failed-SSH. Source website chưa mount/copy/chạy và không bị xoá.
- Artifact: `build/bin/OneClick-Dev-Server.exe`, SHA-256 `CB0EBCD5AE676709E940F52C6FD47DA40DED62FD1793BD2FA114E0FAC7EF4AC0`; chỉ còn một EXE.
- Next: User mở revision 12, chọn project lỗi, bấm `Tiếp tục`, sau đó `Tạo lại từ đầu` và xác nhận để retest fresh VM sau reboot.

### 2026-08-22 — README revision 11 — Chặn kẹt 68% và yêu cầu restart driver

- Changed: Launch progress poll trạng thái VM thật mỗi 5 giây, phân biệt tải image/chờ mạng/hoàn tất; fresh VM đã bật nhưng không IP hoặc SSH trong 2 phút được dừng chờ và trả `rebootRequired` trên Windows VirtualBox. `N/A` không còn được coi là IP.
- Root cause evidence: VM `oneclick-flatsome-4c3c7c2dba` giữ `Starting`, IPv4 rỗng và Multipass log dừng ở `Waiting for SSH to be up`; VM trước đó báo `Running` nhưng IPv4 `N/A` và SSH tới port-forward localhost timeout. Windows uptime 53,4 giờ, chưa restart sau lần cài VirtualBox trong phiên test.
- Installer/UX: Mọi cài VirtualBox thành công đều báo cần restart; UI không refresh readiness để cho đi tiếp trong dialog cài. Lỗi launch mạng hiển thị hướng dẫn restart trực tiếp, ẩn `Thử lại/Tạo lại` để tránh vòng lặp xoá-tạo không sửa được driver.
- Files: `internal/vm/model.go`, `internal/vm/environment.go`, `internal/vm/environment_test.go`, `internal/installer/platform_windows.go`, `frontend/src/App.tsx`, generated bindings/dist, `README.md`, rebuilt single EXE.
- Tests: Unit tests cho `Starting`/`Running + N/A` và recovery theo driver; `go test ./...`, `go vet ./...`, `npm run build`, Wails production build pass; PE subsystem là Windows GUI (`2`).
- Security impact: Không đổi fixed image/resource, no-mount/no-bridge hay confirmed purge guard; monitor mới chỉ gọi `multipass list` read-only và không tự xoá VM. Website vẫn chưa được copy/chạy/public.
- Artifact: `build/bin/OneClick-Dev-Server.exe`, SHA-256 `7EFC7A5D46C7EB1287678DAAE72738E3353F55CEF229A0CA4C10A63330DA9420`; chỉ còn một EXE.
- Next: Restart Windows, mở revision 11 và bấm `Tiếp tục`; nếu VM vẫn không có IP sau restart, thu log boot/network trước khi cho phép recovery tiếp theo.

### 2026-08-22 — README revision 10 — Phục hồi VM kẹt Starting

- Changed: Instance đã `Starting` không bị gọi `multipass start` chồng; app poll tối đa 30 giây với progress rồi trả lỗi recoverable. Thêm confirmation `Tạo lại từ đầu` → `Xoá và tạo lại`; backend xác minh VM name với project history, từ chối VM Running, stop-force/delete-purge đúng VM lỗi rồi launch lại.
- Evidence: VM `oneclick-noibo-kientrucnhacuagio-6be4db4547` giữ `Starting`, không CPU/IP; Windows Event Log của Multipass dừng ở `Waiting for SSH to be up` từ 15:38. Retry cũ chạy `start --timeout 300`, giữ UI ở 35% và tạo spinner noise trong history.
- Fixed additionally: Parser `multipass info --format json` đúng envelope/object của 1.16.3 và `cpu_count` string; CLI output loại chuỗi spinner; start/reset phát progress pulse.
- Files: `app.go`, `internal/vm/model.go`, `internal/vm/environment.go`, `internal/vm/environment_test.go`, `internal/state/repository.go`, `frontend/src/App.tsx`, `frontend/src/App.css`, generated bindings/dist, `README.md`, rebuilt single EXE.
- Tests: Unit tests cho Multipass info 1.16.3 và spinner cleanup; `go test ./...`, `go vet ./...`, `npm run build` và production Wails build pass. Destructive recovery thật chờ user xác nhận trong UI.
- Security impact: Recreate không nhận VM name từ frontend, không xoá VM Running, bắt buộc match record + deterministic plan và confirmation riêng; source project không mount/copy/chạy ở bước này.
- Next: User chạy revision 10, mở project lỗi, bấm tạo môi trường; sau 30 giây chọn `Tạo lại từ đầu` và xác nhận.

### 2026-08-22 — README revision 9 — Ẩn elevated terminal và progress cài VirtualBox

- Changed: Thêm `-WindowStyle Hidden` cho PowerShell chạy sau UAC; giữ UAC có chủ đích. Trong lúc silent installer chạy, backend phát progress mỗi 5 giây và UI thông báo chờ khoảng 2–5 phút, không tạo cảm giác ứng dụng bị treo.
- Evidence: User xác nhận revision 8 cài VirtualBox thành công. Máy test hiện có `VBoxManage 7.2.16r174877`; Multipass báo driver `virtualbox`, service `Running` và CLI list trả JSON hợp lệ.
- Root cause UX: Elevated PowerShell được khởi chạy bằng `Start-Process -Verb RunAs` nhưng thiếu `-WindowStyle Hidden`, nên hiện console trống. Backend đồng thời đứng ở progress 82% trong toàn bộ thời gian chờ silent installer.
- Files: `internal/installer/platform_windows.go`, `internal/installer/platform_windows_test.go`, `frontend/src/App.tsx`, `README.md`, rebuilt `build/bin/OneClick-Dev-Server.exe`.
- Tests: Unit test bắt buộc launcher vừa có `-Verb RunAs` vừa có `-WindowStyle Hidden`; `go test ./...`, `go vet ./...`, `npm run build` và production Wails build pass.
- Security impact: Không thay đổi artifact verification, allowlist hoặc quyền. UAC vẫn bắt buộc; chỉ console của helper sau consent bị ẩn.
- Next: User mở revision 9, bấm `Kiểm tra lại`; nếu toàn bộ sẵn sàng thì tiếp tục `Tạo môi trường`.

### 2026-08-22 — README revision 8 — Sửa kết quả cài VirtualBox sau UAC

- Changed: Elevated VirtualBox helper không còn đọc installer/result/hash từ biến môi trường có thể mất sau UAC. Các giá trị đã xác minh được nhúng an toàn vào encoded command; result journal được tạo trước, ghi UTF-8 không BOM, giữ mã thoát helper và báo lỗi cụ thể nếu không cập nhật.
- Root cause: Luồng revision 7 nâng quyền một PowerShell mới nhưng truyền đường dẫn bộ cài và `result.txt` bằng process environment. Sau UAC, elevated process không được bảo đảm nhận các biến đó; cả thao tác chính lẫn catch writer có thể thiếu result path, tạo thông báo chung `không nhận được kết quả cài đặt VirtualBox`.
- Files: `internal/installer/platform_windows.go`, `internal/installer/platform_windows_test.go`, `README.md`, rebuilt `build/bin/OneClick-Dev-Server.exe`.
- Tests: Unit test xác nhận elevated script không phụ thuộc environment, escape PowerShell literal và pin checksum; `go test ./...`, `go vet ./...`, production Wails build pass. Cài VirtualBox thật chờ user bấm lại để giữ explicit consent.
- Security impact: Không nới allowlist hoặc quyền; installer vẫn phải qua official host, SHA-256 và Oracle Authenticode trước UAC. Chỉ dữ liệu temp đã xác minh được nhúng, với quote escaping và không nhận input từ frontend.
- Artifact: Thay bản cũ bằng một file duy nhất `build/bin/OneClick-Dev-Server.exe`.
- Next: User chạy revision 8 và bấm `Thử lại`; nếu installer trả lỗi, dialog mới sẽ hiện nguyên nhân/mã helper thay vì lỗi mất kết quả.

### 2026-08-22 — README revision 7 — Lịch sử JSON và sửa dependency VirtualBox

- Changed: Persist cấu hình/trạng thái/lịch sử nhiều website vào JSON local; giao diện hiển thị tổng số, trạng thái, bước cuối, lịch sử và CTA tiếp tục/thử lại. Readiness/VM dùng chung backend inspection; Windows Home với Multipass driver `virtualbox` nhưng thiếu `VBoxManage.exe` được chặn sớm và có plan cài VirtualBox đã xác minh.
- Root cause: Máy test dùng Windows Home, Multipass `1.16.3+win` được cấu hình driver `virtualbox` nhưng Oracle VirtualBox/VBoxManage không tồn tại; vì vậy `multipass launch` không thể khởi động process để sinh UUID.
- Files: `app.go`, `internal/state/`, `internal/multipass/`, `internal/readiness/`, `internal/installer/`, `internal/vm/`, `frontend/src/App.tsx`, `frontend/src/App.css`, generated bindings/dist, `README.md`, rebuilt single EXE.
- Tests: `go test ./...`, `go vet ./...`, `npm run build` pass; JSON multi-project/recovery, allowlist và terminal-output cleanup có unit tests. Cài VirtualBox và tạo VM thật chờ user test để không tự ý thay đổi máy trong build session.
- Security impact: State ở user profile, atomic, strict schema/size và không chứa secret/content; hypervisor dependency được fail closed; download VirtualBox bị pin official host + SHA-256 + Oracle Authenticode trước UAC; không đổi no-mount/no-bridge/fixed-resource policy.
- UI/UX: Thêm compact project summary/list, status badge, recent history và resume/retry panel; technical error mặc định thu gọn và loại control/ANSI noise.
- Artifact: Chỉ giữ `build/bin/OneClick-Dev-Server.exe` sau khi xác minh build mới.
- Next: User chạy revision 7, chọn lại/lưu website một lần để tạo lịch sử ban đầu, cài VirtualBox từ `Kiểm tra máy`, rồi thử `Tạo môi trường`.

### 2026-08-22 — README revision 6 — Tương thích Multipass 1.16.3

- Changed: Bỏ command `multipass wait-ready` không tồn tại ở bản 1.16.3; thay bằng polling `multipass list --format json`, vừa tương thích bản pin vừa xác minh daemon thực sự trả lời.
- Files: `internal/vm/environment.go`, `README.md`, rebuilt `build/bin/OneClick-Dev-Server.exe`.
- Tests: CLI thật trên máy user xác nhận `multipass 1.16.3+win`, `list/launch/start/exec/info` và toàn bộ flags đang dùng đều hợp lệ; `go test ./...`, `go vet ./...`, production frontend/Wails build pass.
- Security impact: Không đổi launch allowlist, fixed resources, no-mount/no-bridge hoặc health-check; service probe vẫn read-only và child process vẫn hidden/no-window.
- Artifact: Ghi đè bản sửa vào file duy nhất `build/bin/OneClick-Dev-Server.exe`.
- Next: User đóng bản ứng dụng cũ, chạy lại EXE revision 6 và thử `Tạo môi trường`.

### 2026-08-22 — README revision 5 — Tạo môi trường Multipass an toàn

- Changed: Nút `Tạo môi trường` mở màn xác nhận rồi tạo hoặc dùng lại dedicated Ubuntu 24.04 VM; progress chạy nền không bật terminal; kiểm tra OneClick marker, Ubuntu/architecture, trạng thái, CPU và không có host mount trước khi báo sẵn sàng.
- Files: `app.go`, `internal/vm/`, `frontend/src/App.tsx`, `frontend/src/App.css`, generated Wails bindings/dist, `README.md`.
- Tests: `go test ./...`, `go vet ./...`, `npm run build` và Wails Windows production build pass; runtime VM đang chờ user test để tránh tự tạo máy ảo trên máy người dùng trong build session.
- Security impact: Multipass command/image/resources là allowlist cố định trong Go; instance name deterministic; không dùng `--mount`/bridged network; website chưa được copy/chạy; instance lạ hoặc có mount bị fail closed và không tự xoá.
- UI/UX: CTA thật thay nhãn `Sắp triển khai`; dialog CRM compact hiển thị VM/image/CPU/RAM/disk/NAT/no-mount, có disabled/loading/success/error/retry và bước tiếp theo rõ ràng.
- Artifact: Chỉ giữ `build/bin/OneClick-Dev-Server.exe`; các EXE cũ được xoá sau khi xác minh build mới.
- Next: User test tạo/dùng lại VM trên Windows; sau đó snapshot/copy website, guest runtime và firewall trước khi chạy mã dự án.

### 2026-08-22 — README revision 4 — Ẩn terminal và nối luồng cấu hình website

- Changed: Mọi PowerShell/Multipass child process trên Windows chạy hidden/no-window; sau folder picker ứng dụng nhận diện dự án và tự mở wizard cấu hình 3 bước; nút project card đổi thành `Cấu hình/Xem cấu hình` có hành động thật.
- Files: `app.go`, `internal/hostexec/`, `internal/project/`, `internal/readiness/`, `internal/installer/platform_windows.go`, `frontend/src/App.tsx`, `frontend/src/App.css`, generated Wails bindings, `README.md`.
- Tests: `go test ./...` và `go vet ./...` pass; Linux/amd64 internal packages cross-build pass; `npm run build` pass; Wails Windows executable `oneclick-dev-server-r4.exe` build pass.
- Security impact: Detector không recurse/chạy/sửa website; child process giữ nguyên allowlist/consent; document root traversal bị chặn ở UI và bắt buộc tái kiểm tra backend trước snapshot.
- UI/UX: Wizard hiển thị rõ `Chọn website → Cấu hình → Tạo môi trường`, tự đi vào bước 2 sau khi chọn folder, có validation và confirmation; không còn nút `Tiếp tục` rỗng.
- Next: User test terminal suppression và project detection/config; hiện thực dedicated VM health-check và persist deployment state.

### 2026-08-22 — README revision 3 — CRM readiness và cài Multipass

- Changed: Chuyển màn Kiểm tra máy sang template CRM dạng bảng compact; nối nút `Cài đặt/Sửa` với luồng cài Multipass có confirm, progress, success/error và postflight readiness.
- Files: `app.go`, `internal/readiness/`, `internal/installer/`, `frontend/src/App.tsx`, `frontend/src/App.css`, generated Wails bindings, `README.md`.
- Tests: `go test ./...` và `go vet ./...` pass; Linux/amd64 installer package cross-build pass; `npm run build` pass; Wails Windows production executable `oneclick-dev-server-crm.exe` build pass.
- Security impact: Frontend chỉ gửi action allowlist; Windows pin Multipass 1.16.3, giới hạn nguồn HTTPS GitHub, bắt buộc Authenticode hợp lệ từ Canonical trước UAC/msiexec; không tự chạy nếu chưa confirm.
- UI/UX: Sidebar sáng, bảng readiness mật độ CRM, mô tả ngắn, dialog cài đặt có publisher/version/source/admin và trạng thái lỗi phục hồi được.
- Next: User test cài Multipass/UAC trên Windows; thêm kiểm tra edition/backend và launch VM health-check.

### 2026-08-22 — README revision 2 — Desktop GUI và readiness MVP

- Changed: Chuyển sản phẩm chính từ CLI sang Wails desktop GUI cho nhân viên; thêm Setup/Overview/Website UI và readiness engine Windows/Linux.
- Files: Wails/Go scaffold, `internal/readiness/`, React frontend, build config, dependency locks, `.gitignore`, `README.md`.
- Tests: `go test ./...` pass; `npm run build` pass; Wails Windows production executable build pass.
- Security impact: Readiness engine chỉ đọc; deploy/select project bị khoá khi required checks chưa sẵn sàng; chưa triển khai mutation/installer.
- UI/UX: Enterprise operations tối giản, tiếng Việt mặc định, SVG icons, 44px controls, focus states, reduced motion và technical-detail disclosure.
- Next: Setup Wizard thực thi installation plan bằng consent và scoped elevation.

### 2026-08-22 — README revision 1 — Khởi tạo đặc tả

- Changed: Tạo nguồn sự thật trung tâm cho kiến trúc, security, preflight/install, deploy lifecycle và roadmap.
- Files: `README.md`.
- Tests: Chưa có mã nguồn để chạy test; đã rà soát tính nhất quán của đặc tả.
- Security impact: Xác lập VM isolation, no-host-mount, egress control và explicit-consent installer làm invariant.
- Next: Scaffold Go CLI và hiện thực `oneclick doctor` read-only.
