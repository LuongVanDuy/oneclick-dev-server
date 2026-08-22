# Isolation architecture

## Threat model

OneClick Dev Server treats every selected web project as untrusted. There is no malware-scanning gate before launch. Safety comes from containment rather than trust in the source code.

The design target is strong isolation, not an absolute claim that a hypervisor can never have a vulnerability.

## Core invariant: one project, one VM

Each project receives its own Hyper-V VM and its own writable differencing VHDX based on an immutable base image.

The selected XAMPP or Laragon directory on the Windows host is a source only. It is never mounted into the guest and is never used as the guest document root.

```text
Windows host
  C:\xampp\htdocs\site-a   (source only)
  C:\laragon\www\site-b    (source only)

          read/copy only
                |
                v
  +----------------------+    +----------------------+
  | VM: site-a           |    | VM: site-b           |
  | private VHDX         |    | private VHDX         |
  | private web runtime  |    | private web runtime  |
  | private database     |    | private database     |
  | private tunnel       |    | private tunnel       |
  +----------------------+    +----------------------+
       X no shared disk          X no shared disk
       X no shared database      X no VM-to-VM traffic
```

## Host boundary

A guest must not receive any of these host integrations:

- no shared host folders or host drive mapping
- no clipboard or Enhanced Session dependency
- no host credentials, SSH keys, browser profiles, or Git credentials
- no shared MySQL instance
- no shared Apache/Nginx/PHP process
- no direct access to the XAMPP/Laragon project directory

Project synchronization is a one-way deployment operation controlled by the host application. The guest receives a copy of the source; it does not receive a writable reference back to the original source tree.

## Disk isolation

A read-only/golden base VM image is maintained by OneClick Dev Server. Creating a site creates a dedicated differencing VHDX. Destroying a site removes only that site's child disk.

No site shares a writable virtual disk with another site.

## Network isolation

All site VMs use a dedicated Hyper-V NAT network for outbound Internet access. Hyper-V network adapter ACLs are applied so a site VM cannot initiate connections to:

- the Windows host management addresses
- RFC1918/private LAN ranges
- another site VM's address

The guest needs outbound Internet only for normal application dependencies and the public tunnel. No inbound router port-forward is required.

IPv6 must either receive equivalent isolation policy or be disabled for the sandbox network so it cannot bypass IPv4 rules.

Each public tunnel runs inside the corresponding site VM. A compromised site therefore does not obtain a tunnel process running on the Windows host.

## Site lifecycle

1. Discover XAMPP/Laragon projects.
2. User selects a project.
3. Create a dedicated child VHDX and VM from the golden base image.
4. Apply VM hardening and network ACLs.
5. Copy a snapshot of the selected project into the guest.
6. Start the guest's web/database stack.
7. Start the guest's Cloudflare Tunnel when public access is requested.
8. For source changes, perform an explicit one-way sync into that VM.
9. Stop, reset, or destroy the VM independently of every other site.

## Failure containment

If site A executes malicious PHP or another payload, the expected blast radius is site A's VM and its private data. Site B uses a different VM, disk, database, runtime, and tunnel and therefore does not share the application-level compromise.

Hyper-V remains a security boundary, so this design must still account for hypervisor/host vulnerabilities and keep Windows/Hyper-V patched. The application must not describe VM isolation as mathematically absolute.
