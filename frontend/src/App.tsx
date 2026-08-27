import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {FormEvent, ReactNode} from 'react';
import './App.css';
import {ConfigureDataStorage, CopyWebsite, CreateEnvironment, CreateProjectBackup, DetectProjectDirectory, DiscoverProjectDirectories, GetCloudflareConnections, GetCopyPlan, GetDatabasePlan, GetDataStorageStatus, GetDependencyPlan, GetEnvironmentPlan, GetLegacyRecoveryImportPlan, GetProjectDataStatus, GetProjects, GetRecoveryBackupPlan, GetRecoveryProjects, GetRecoveryToolStatus, GetRuntimePlan, GetTunnelPlan, ImportDatabase, ImportLegacyRecoveryData, InstallRuntime, ListProjectSource, OpenExternalURL, OpenProjectBackupFolder, OpenProjectSource, OpenRecoveryFolder, PrepareRecoveryTool, ReadProjectSourceFile, RecreateEnvironment, RemoveCloudflareConnection, RemoveRecoveryProject, RunDependencyAction, RunReadinessChecks, RunRecoveryBackup, SaveCloudflareConnection, SaveProjectConfiguration, SaveProjectSourceFile, SaveRecoveryProject, SelectDatabaseFile, SelectDataStorageDirectory, SelectLegacyRecoveryDirectory, SelectProjectDirectory, StartNamedTunnel, StartRecoveryTool, StopNamedTunnel, TestRecoveryConnection} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';

type CheckStatus = 'ready' | 'installable' | 'user_action' | 'unsupported' | 'broken';

type ReadinessCheck = {
    id: string;
    title: string;
    status: CheckStatus;
    summary: string;
    detail?: string;
    actionLabel?: string;
    actionKind?: string;
    required: boolean;
};

type ReadinessReport = {
    platform: string;
    arch: string;
    ready: boolean;
    checkedAt: string;
    checks: ReadinessCheck[];
};

type DependencyPlan = {
    action: string;
    title: string;
    description: string;
    publisher: string;
    version: string;
    source: string;
    downloadSize: string;
    requiresAdmin: boolean;
    rebootPossible: boolean;
    supported: boolean;
};

type InstallerProgress = {stage: string; message: string; percent: number};
type InstallerResult = {success: boolean; rebootRequired: boolean; message: string; detail?: string};
type EnvironmentPlan = {vmName: string; image: string; cpus: number; memory: string; disk: string; network: string; isolation: string; existing: boolean};
type EnvironmentProgress = {stage: string; message: string; percent: number};
type EnvironmentResult = {success: boolean; reused: boolean; canRecreate?: boolean; rebootRequired?: boolean; message: string; detail?: string; vmName?: string; state?: string; ip?: string; release?: string};
type CopyPlan = {projectPath: string; fileCount: number; totalBytes: number; excludedCount: number; secretExcluded: number; policy: string};
type CopyProgress = {stage: string; message: string; percent: number};
type CopyResult = {success: boolean; message: string; detail?: string; snapshotId?: string; checksum?: string; fileCount?: number; totalBytes?: number; guestPath?: string};
type RuntimePlan = {projectPath: string; adapter: string; runtime: string; database: string; services: number; packages: string[]; download: string; resources: string; network: string; public: boolean; existing: boolean};
type RuntimeProgress = {stage: string; message: string; percent: number};
type RuntimeResult = {success: boolean; message: string; detail?: string; adapter?: string; health?: string; services?: number; snapshotId?: string; runtimeState?: string};
type DatabaseMode = 'automatic' | 'file';
type DatabasePlan = {projectPath: string; mode: DatabaseMode; ready: boolean; source: string; database?: string; tablePrefix?: string; tool?: string; importPath?: string; importBytes?: number; automaticMessage?: string; replacementNotice: string};
type DatabaseProgress = {stage: string; message: string; percent: number};
type DatabaseResult = {success: boolean; message: string; detail?: string; source?: string; tables?: number; bytes?: number; tablePrefix?: string};
type CloudflareStatus = {connected: boolean; tokenStored: boolean; zone: string; zoneId?: string; accountId?: string; zoneStatus?: string; assignedNameServers?: string[]; activeNameServers?: string[]; dnsReady: boolean; message: string};
type DataStorageStatus = {platform: string; supported: boolean; configured: boolean; locked: boolean; canConfigure: boolean; requiresAdmin: boolean; currentPath: string; defaultPath: string; recommendedPath: string; freeBytes: number; instanceCount: number; projectCount: number; message: string; detail?: string};
type DataStorageProgress = {stage: string; message: string; percent: number};
type DataStorageResult = {success: boolean; message: string; detail?: string; status: DataStorageStatus};
type RecoveryToolStatus = {state: string; message: string; detail?: string; url?: string; version?: string; dataPath?: string; ready: boolean; running: boolean; setupRequired: boolean};
type RecoveryToolProgress = {stage: string; message: string; percent: number};
type LegacyImportPlan = {sourcePath: string; destinationPath: string; files: number; bytes: number; profiles: number; secrets: number; existingFiles: number; conflicts: number; ready: boolean; message: string; detail?: string};
type LegacyImportProgress = {stage: string; message: string; percent: number; filesDone: number; filesTotal: number; bytesDone: number; bytesTotal: number};
type LegacyImportResult = {success: boolean; message: string; detail?: string; files: number; bytes: number; skipped: number; destinationPath?: string};
type TunnelPlan = {projectPath: string; hostname: string; provider: string; mode: string; address: string; image: string; download: string; network: string; exposure: string; existing: boolean};
type TunnelProgress = {stage: string; message: string; percent: number};
type TunnelResult = {success: boolean; message: string; detail?: string; url?: string; state?: string; health?: string};
type ProjectInfo = {path: string; name: string; kind: string; kindLabel: string; documentRoot: string; phpRequirement?: string; ready: boolean; warnings: string[]};
type ProjectSettings = {name: string; kind: string; documentRoot: string; phpVersion: string; tunnelMode: 'named'};
type ProjectHistoryEntry = {id: string; action: string; status: string; message: string; detail?: string; createdAt: string};
type StoredProject = {id: string; path: string; name: string; kind: string; kindLabel: string; documentRoot: string; phpVersion: string; tunnelMode: 'quick' | 'named'; stage: string; status: string; vmName?: string; vmState?: string; ip?: string; snapshotId?: string; snapshotHash?: string; snapshotPath?: string; snapshotFiles?: number; snapshotBytes?: number; runtimeAdapter?: string; runtimeState?: string; runtimeHealth?: string; runtimeContainers?: number; runtimeSnapshotId?: string; databaseState?: string; databaseSource?: string; databaseTables?: number; databaseBytes?: number; databasePrefix?: string; databaseImportedAt?: string; domainZone?: string; hostname?: string; tunnelId?: string; dnsRecordId?: string; tunnelState?: string; tunnelHealth?: string; tunnelUrl?: string; tunnelStartedAt?: string; lastError?: string; createdAt: string; updatedAt: string; history: ProjectHistoryEntry[]};
type ProjectSnapshot = {schema: number; updatedAt: string; storagePath: string; domains?: unknown[]; projects: StoredProject[]};
type ProjectDataStatus = {projectPath: string; sourcePath: string; deployedPath: string; database: string; databaseTables: number; databaseImportedAt?: string; backupRoot: string; backupCount: number; lastBackupAt?: string; lastBackupPath?: string; canBackup: boolean; canReplaceDatabase: boolean; message: string};
type BackupProgress = {stage: string; message: string; percent: number};
type BackupResult = {success: boolean; message: string; detail?: string; path?: string; sourceBytes?: number; databaseBytes?: number; createdAt?: string};
type SourceEntry = {name: string; path: string; kind: 'directory' | 'file' | 'blocked'; size: number; modifiedAt?: string; editable: boolean; blockedReason?: string};
type SourceListing = {projectPath: string; directory: string; parent?: string; entries: SourceEntry[]};
type SourceFile = {path: string; name: string; content: string; sha256: string; bytes: number; modifiedAt: string};
type SourceSaveResult = {success: boolean; message: string; detail?: string; path?: string; sha256?: string; bytes?: number; backupPath?: string; savedAt?: string};
type RecoveryHistoryEntry = {id: string; action: string; status: string; message: string; detail?: string; createdAt: string};
type RecoveryProject = {id: string; name: string; host: string; username: string; protocol: 'ftps' | 'ftp'; port: number; remotePath: string; siteUrl?: string; workers: number; passive: boolean; status: string; lastError?: string; lastCheckedAt?: string; lastBackupId?: string; backupFiles?: number; backupBytes?: number; findingCount?: number; criticalCount?: number; backupCreatedAt?: string; createdAt: string; updatedAt: string; history: RecoveryHistoryEntry[]; passwordStored: boolean; dataPath?: string};
type RecoverySnapshot = {schema: number; updatedAt: string; storagePath: string; projects: RecoveryProject[]};
type RecoveryProjectInput = {id: string; name: string; host: string; username: string; password: string; protocol: 'ftps' | 'ftp'; port: number; remotePath: string; siteUrl: string; workers: number; passive: boolean; allowInsecure: boolean};
type RecoveryConnectionResult = {success: boolean; message: string; detail?: string; remotePath?: string; secure: boolean; checkedAt?: string};
type RecoveryBackupPlan = {projectId: string; name: string; host: string; protocol: string; remotePath: string; destination: string; workers: number; readOnly: boolean; storageMode: string};
type RecoveryBackupProgress = {stage: string; message: string; percent: number; filesDone?: number; filesTotal?: number; bytesDone?: number; bytesTotal?: number};
type RecoveryBackupResult = {success: boolean; message: string; detail?: string; backupId?: string; path?: string; manifestPath?: string; reportPath?: string; files?: number; bytes?: number; findings?: number; critical?: number; createdAt?: string};
type View = 'overview' | 'websites' | 'domains' | 'fresh-wordpress' | 'recovery' | 'system' | 'settings';
type WebsiteScreen = 'list' | 'configure' | 'detail' | 'source';
type IconName = 'home' | 'globe' | 'link' | 'recovery' | 'shield' | 'settings' | 'check' | 'warning' | 'close' | 'refresh' | 'folder' | 'file' | 'lock' | 'database' | 'archive' | 'arrow' | 'back' | 'info' | 'history';

const statusCopy: Record<CheckStatus, {label: string; icon: IconName}> = {
    ready: {label: 'Sẵn sàng', icon: 'check'},
    installable: {label: 'Cần cài đặt', icon: 'arrow'},
    user_action: {label: 'Cần xử lý', icon: 'warning'},
    unsupported: {label: 'Chưa hỗ trợ', icon: 'close'},
    broken: {label: 'Có lỗi', icon: 'close'},
};

function Icon({name, size = 20}: {name: IconName; size?: number}) {
    const paths: Record<IconName, ReactNode> = {
        home: <><path d="M3 10.5 12 3l9 7.5"/><path d="M5 9.5V21h14V9.5"/><path d="M9 21v-7h6v7"/></>,
        globe: <><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18"/></>,
        link: <><path d="M10 13a5 5 0 0 0 7.5.5l2-2a5 5 0 0 0-7-7l-1.2 1.2"/><path d="M14 11a5 5 0 0 0-7.5-.5l-2 2a5 5 0 0 0 7 7l1.2-1.2"/></>,
        recovery: <><path d="M20 7v5h-5"/><path d="M18.5 16a8 8 0 1 1 .6-7.2L20 11"/><path d="m9 12 2 2 4-5"/></>,
        shield: <><path d="M12 3 4.5 6v5.5c0 4.8 3.1 8 7.5 9.5 4.4-1.5 7.5-4.7 7.5-9.5V6L12 3Z"/><path d="m8.8 12 2.1 2.1 4.5-4.6"/></>,
        settings: <><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3V2.8h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z"/></>,
        check: <path d="m5 12 4 4L19 6"/>,
        warning: <><path d="M10.3 3.8 2.6 17.1A2 2 0 0 0 4.3 20h15.4a2 2 0 0 0 1.7-2.9L13.7 3.8a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4M12 17h.01"/></>,
        close: <><path d="m7 7 10 10M17 7 7 17"/></>,
        refresh: <><path d="M20 6v5h-5"/><path d="M18.5 15a7 7 0 1 1-.7-7.8L20 10"/></>,
        folder: <path d="M3 6.5h6l2 2h10v10.5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6.5Z"/>,
        file: <><path d="M6 2h8l4 4v16H6z"/><path d="M14 2v5h5M9 12h6M9 16h6"/></>,
        lock: <><rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/></>,
        database: <><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v7c0 1.7 3.6 3 8 3s8-1.3 8-3V5"/><path d="M4 12v7c0 1.7 3.6 3 8 3s8-1.3 8-3v-7"/></>,
        archive: <><path d="M4 7h16v14H4zM3 3h18v4H3z"/><path d="M9 11h6"/></>,
        arrow: <><path d="M5 12h14M14 7l5 5-5 5"/></>,
        back: <><path d="M19 12H5M10 7l-5 5 5 5"/></>,
        info: <><circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/></>,
        history: <><path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5M12 7v5l3 2"/></>,
    };

    return <svg className="icon" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>;
}

function useDialogFocus(onClose: () => void, canClose: boolean) {
    const dialogRef = useRef<HTMLElement>(null);
    const closeRef = useRef(onClose);
    const canCloseRef = useRef(canClose);

    useEffect(() => { closeRef.current = onClose; }, [onClose]);
    useEffect(() => { canCloseRef.current = canClose; }, [canClose]);
    useEffect(() => {
        const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const frame = window.requestAnimationFrame(() => {
            const dialog = dialogRef.current;
            const preferred = dialog?.querySelector<HTMLElement>('[autofocus], input:not([disabled]), button:not([disabled]), select:not([disabled]), [href], [tabindex]:not([tabindex="-1"])');
            (preferred || dialog)?.focus();
        });
        const handleKeyDown = (event: KeyboardEvent) => {
            const dialog = dialogRef.current;
            if (!dialog) return;
            if (event.key === 'Escape' && canCloseRef.current) {
                event.preventDefault();
                closeRef.current();
                return;
            }
            if (event.key !== 'Tab') return;
            const focusable = Array.from(dialog.querySelectorAll<HTMLElement>('button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'))
                .filter(element => element.offsetParent !== null);
            if (focusable.length === 0) {
                event.preventDefault();
                dialog.focus();
                return;
            }
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            if (event.shiftKey && document.activeElement === first) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && document.activeElement === last) {
                event.preventDefault();
                first.focus();
            }
        };
        document.addEventListener('keydown', handleKeyDown);
        return () => {
            window.cancelAnimationFrame(frame);
            document.removeEventListener('keydown', handleKeyDown);
            if (previous?.isConnected) previous.focus();
        };
    }, []);

    return dialogRef;
}

function App() {
    const [view, setView] = useState<View>('system');
    const [report, setReport] = useState<ReadinessReport | null>(null);
    const [loading, setLoading] = useState(true);
    const [notice, setNotice] = useState('');
    const [selectedProject, setSelectedProject] = useState('');
    const [projectInfo, setProjectInfo] = useState<ProjectInfo | null>(null);
    const [projectSettings, setProjectSettings] = useState<ProjectSettings | null>(null);
    const [projectConfigured, setProjectConfigured] = useState(false);
    const [projectLoading, setProjectLoading] = useState(false);
    const [websiteScreen, setWebsiteScreen] = useState<WebsiteScreen>('list');
    const [installPlan, setInstallPlan] = useState<DependencyPlan | null>(null);
    const [installProgress, setInstallProgress] = useState<InstallerProgress | null>(null);
    const [installResult, setInstallResult] = useState<InstallerResult | null>(null);
    const [installing, setInstalling] = useState(false);
    const [environmentPlan, setEnvironmentPlan] = useState<EnvironmentPlan | null>(null);
    const [environmentProgress, setEnvironmentProgress] = useState<EnvironmentProgress | null>(null);
    const [environmentResult, setEnvironmentResult] = useState<EnvironmentResult | null>(null);
    const [environmentInfo, setEnvironmentInfo] = useState<EnvironmentResult | null>(null);
    const [environmentPlanning, setEnvironmentPlanning] = useState(false);
    const [creatingEnvironment, setCreatingEnvironment] = useState(false);
    const [copyPlan, setCopyPlan] = useState<CopyPlan | null>(null);
    const [copyProgress, setCopyProgress] = useState<CopyProgress | null>(null);
    const [copyResult, setCopyResult] = useState<CopyResult | null>(null);
    const [copyPlanning, setCopyPlanning] = useState(false);
    const [copying, setCopying] = useState(false);
    const [runtimePlan, setRuntimePlan] = useState<RuntimePlan | null>(null);
    const [runtimeProgress, setRuntimeProgress] = useState<RuntimeProgress | null>(null);
    const [runtimeResult, setRuntimeResult] = useState<RuntimeResult | null>(null);
    const [runtimePlanning, setRuntimePlanning] = useState(false);
    const [installingRuntime, setInstallingRuntime] = useState(false);
    const [databasePlan, setDatabasePlan] = useState<DatabasePlan | null>(null);
    const [databaseMode, setDatabaseMode] = useState<DatabaseMode>('automatic');
    const [databaseImportPath, setDatabaseImportPath] = useState('');
    const [databaseProgress, setDatabaseProgress] = useState<DatabaseProgress | null>(null);
    const [databaseResult, setDatabaseResult] = useState<DatabaseResult | null>(null);
    const [databasePlanning, setDatabasePlanning] = useState(false);
    const [importingDatabase, setImportingDatabase] = useState(false);
    const [cloudflareDomains, setCloudflareDomains] = useState<CloudflareStatus[]>([]);
    const [cloudflareLoading, setCloudflareLoading] = useState(true);
    const [dataStorageStatus, setDataStorageStatus] = useState<DataStorageStatus | null>(null);
    const [dataStorageLoading, setDataStorageLoading] = useState(true);
    const [tunnelDialogOpen, setTunnelDialogOpen] = useState(false);
    const [tunnelZone, setTunnelZone] = useState('');
    const [tunnelSubdomain, setTunnelSubdomain] = useState('');
    const [tunnelPlan, setTunnelPlan] = useState<TunnelPlan | null>(null);
    const [tunnelProgress, setTunnelProgress] = useState<TunnelProgress | null>(null);
    const [tunnelResult, setTunnelResult] = useState<TunnelResult | null>(null);
    const [tunnelPlanning, setTunnelPlanning] = useState(false);
    const [startingTunnel, setStartingTunnel] = useState(false);
    const [stoppingTunnel, setStoppingTunnel] = useState(false);
    const [projects, setProjects] = useState<StoredProject[]>([]);
    const [projectsLoading, setProjectsLoading] = useState(true);
    const [discoveredProjects, setDiscoveredProjects] = useState<ProjectInfo[]>([]);
    const [discoveryLoading, setDiscoveryLoading] = useState(true);
    const [storagePath, setStoragePath] = useState('');
    const [projectDataStatus, setProjectDataStatus] = useState<ProjectDataStatus | null>(null);
    const [projectDataLoading, setProjectDataLoading] = useState(false);
    const [backupProgress, setBackupProgress] = useState<BackupProgress | null>(null);
    const [backupResult, setBackupResult] = useState<BackupResult | null>(null);
    const [backingUp, setBackingUp] = useState(false);
    const [recoverySnapshot, setRecoverySnapshot] = useState<RecoverySnapshot>({schema: 1, updatedAt: '', storagePath: '', projects: []});
    const [recoveryLoading, setRecoveryLoading] = useState(true);
    const projectDataRequest = useRef(0);

    const refresh = useCallback(async () => {
        setLoading(true);
        setNotice('');
        try {
            setReport(await RunReadinessChecks() as ReadinessReport);
        } catch (error) {
            setNotice(`Không thể kiểm tra hệ thống: ${String(error)}`);
        } finally {
            setLoading(false);
        }
    }, []);

    const refreshProjects = useCallback(async () => {
        setProjectsLoading(true);
        try {
            const snapshot = await GetProjects() as ProjectSnapshot;
            setProjects(snapshot.projects || []);
            setStoragePath(snapshot.storagePath || '');
        } catch (error) {
            setNotice(`Không đọc được lịch sử dự án: ${cleanError(error)}`);
        } finally {
            setProjectsLoading(false);
        }
    }, []);

    const refreshCloudflare = useCallback(async () => {
        setCloudflareLoading(true);
        try {
            setCloudflareDomains(await GetCloudflareConnections() as CloudflareStatus[]);
        } catch (error) {
            setCloudflareDomains([]);
            setNotice(`Không đọc được danh sách domain: ${cleanError(error)}`);
        } finally {
            setCloudflareLoading(false);
        }
    }, []);

    const refreshDataStorage = useCallback(async () => {
        setDataStorageLoading(true);
        try {
            setDataStorageStatus(await GetDataStorageStatus() as DataStorageStatus);
        } catch (error) {
            setNotice(`Không đọc được nơi lưu dữ liệu: ${cleanError(error)}`);
        } finally {
            setDataStorageLoading(false);
        }
    }, []);

    const refreshDiscovery = useCallback(async () => {
        setDiscoveryLoading(true);
        try {
            setDiscoveredProjects(await DiscoverProjectDirectories() as ProjectInfo[]);
        } catch (error) {
            setNotice(`Không tự nhận được website: ${cleanError(error)}`);
        } finally {
            setDiscoveryLoading(false);
        }
    }, []);

    const refreshRecovery = useCallback(async () => {
        setRecoveryLoading(true);
        try {
            const snapshot = await GetRecoveryProjects() as RecoverySnapshot;
            setRecoverySnapshot({...snapshot, projects: snapshot.projects || []});
        } catch (error) {
            setNotice(`Không đọc được danh sách khôi phục: ${cleanError(error)}`);
        } finally {
            setRecoveryLoading(false);
        }
    }, []);

    const loadProjectData = useCallback(async (projectPath: string) => {
        const request = ++projectDataRequest.current;
        setProjectDataLoading(true);
        try {
            const status = await GetProjectDataStatus(projectPath) as ProjectDataStatus;
            if (request === projectDataRequest.current) setProjectDataStatus(status);
        } catch (error) {
            if (request === projectDataRequest.current) {
                setProjectDataStatus(null);
                setNotice(`Không đọc được dữ liệu website: ${cleanError(error)}`);
            }
        } finally {
            if (request === projectDataRequest.current) setProjectDataLoading(false);
        }
    }, []);

    useEffect(() => { void refresh(); }, [refresh]);
    useEffect(() => { void refreshProjects(); }, [refreshProjects]);
    useEffect(() => { void refreshCloudflare(); }, [refreshCloudflare]);
    useEffect(() => { void refreshDataStorage(); }, [refreshDataStorage]);
    useEffect(() => { void refreshDiscovery(); }, [refreshDiscovery]);
    useEffect(() => { void refreshRecovery(); }, [refreshRecovery]);
    useEffect(() => EventsOn('installer:progress', (value: InstallerProgress) => setInstallProgress(value)), []);
    useEffect(() => EventsOn('environment:progress', (value: EnvironmentProgress) => setEnvironmentProgress(value)), []);
    useEffect(() => EventsOn('copy:progress', (value: CopyProgress) => setCopyProgress(value)), []);
    useEffect(() => EventsOn('runtime:progress', (value: RuntimeProgress) => setRuntimeProgress(value)), []);
    useEffect(() => EventsOn('database:progress', (value: DatabaseProgress) => setDatabaseProgress(value)), []);
    useEffect(() => EventsOn('tunnel:progress', (value: TunnelProgress) => setTunnelProgress(value)), []);
    useEffect(() => EventsOn('backup:progress', (value: BackupProgress) => setBackupProgress(value)), []);
    useEffect(() => {
        const closeOnEscape = (event: KeyboardEvent) => {
            if (event.key !== 'Escape') return;
            if (installPlan && !installing) closeInstallDialog();
            if (environmentPlan && !creatingEnvironment) closeEnvironmentDialog();
            if (copyPlan && !copying) closeCopyDialog();
            if (runtimePlan && !installingRuntime) closeRuntimeDialog();
            if (databasePlan && !importingDatabase) closeDatabaseDialog();
            if (tunnelDialogOpen && !startingTunnel) closeTunnelDialog();
        };
        window.addEventListener('keydown', closeOnEscape);
        return () => window.removeEventListener('keydown', closeOnEscape);
    }, [installPlan, installing, environmentPlan, creatingEnvironment, copyPlan, copying, runtimePlan, installingRuntime, databasePlan, importingDatabase, tunnelDialogOpen, startingTunnel]);

    const progress = useMemo(() => {
        if (!report) return {ready: 0, total: 0, percent: 0};
        const required = report.checks.filter(check => check.required);
        const ready = required.filter(check => check.status === 'ready').length;
        return {ready, total: required.length, percent: required.length ? Math.round(ready / required.length * 100) : 0};
    }, [report]);

    const newDiscoveredProjects = useMemo(
        () => discoveredProjects.filter(info => !projects.some(record => samePath(record.path, info.path))),
        [discoveredProjects, projects],
    );
    const readyCloudflareDomains = useMemo(() => cloudflareDomains.filter(domain => domain.connected && domain.dnsReady && domain.tokenStored), [cloudflareDomains]);

    function openDetectedProject(info: ProjectInfo) {
        if (!report?.ready) {
            setView('system');
            setNotice('Hoàn tất các mục bắt buộc trước khi cấu hình website.');
            return;
        }
        setSelectedProject(info.path);
        setProjectInfo(info);
        setProjectSettings({
            name: info.name,
            kind: info.kind,
            documentRoot: info.documentRoot,
            phpVersion: info.kind === 'static' ? 'none' : 'auto',
            tunnelMode: 'named',
        });
        setProjectConfigured(false);
        setEnvironmentPlan(null);
        setEnvironmentProgress(null);
        setEnvironmentResult(null);
        setEnvironmentInfo(null);
        setCopyPlan(null);
        setCopyProgress(null);
        setCopyResult(null);
        setRuntimePlan(null);
        setRuntimeProgress(null);
        setRuntimeResult(null);
        setDatabasePlan(null);
        setDatabaseProgress(null);
        setDatabaseResult(null);
        projectDataRequest.current++;
        setProjectDataStatus(null);
        setProjectDataLoading(false);
        setBackupProgress(null);
        setBackupResult(null);
        setTunnelDialogOpen(false);
        setTunnelPlan(null);
        setTunnelProgress(null);
        setTunnelResult(null);
        setWebsiteScreen('configure');
        setView('websites');
    }

    const chooseProject = async () => {
        if (!report?.ready) {
            setView('system');
            setNotice('Hoàn tất các mục bắt buộc trước khi chọn website.');
            return;
        }
        try {
            const path = await SelectProjectDirectory();
            if (path) {
                setProjectLoading(true);
                const info = await DetectProjectDirectory(path) as ProjectInfo;
                const existing = projects.find(item => samePath(item.path, info.path));
                if (existing) {
                    openStoredProject(existing);
                    setNotice('Đã mở dự án từ lịch sử.');
                    return;
                }
                openDetectedProject(info);
            }
        } catch (error) {
            setNotice(`Không đọc được website: ${String(error)}`);
        } finally {
            setProjectLoading(false);
        }
    };

    const confirmProjectSettings = async (settings: ProjectSettings) => {
        if (!projectInfo) throw new Error('Chưa chọn website.');
        const saved = await SaveProjectConfiguration({
            path: projectInfo.path,
            name: settings.name,
            kind: settings.kind,
            documentRoot: settings.documentRoot,
            phpVersion: settings.phpVersion,
            tunnelMode: settings.tunnelMode,
        }) as StoredProject;
        setProjectSettings(settings);
        setProjectConfigured(true);
        setSelectedProject(saved.path);
        await refreshProjects();
        await refreshDiscovery();
        openStoredProject(saved);
        setNotice('Đã lưu cấu hình. Bạn có thể tiếp tục trong chi tiết website.');
    };

    function openStoredProject(record: StoredProject) {
        setSelectedProject(record.path);
        setProjectInfo({
            path: record.path, name: record.name, kind: record.kind, kindLabel: record.kindLabel,
            documentRoot: record.documentRoot, ready: true, warnings: [],
        });
        setProjectSettings({name: record.name, kind: record.kind, documentRoot: record.documentRoot, phpVersion: record.phpVersion, tunnelMode: 'named'});
        setProjectConfigured(true);
        setEnvironmentInfo(hasEnvironment(record.stage) ? {
            success: true, reused: true, message: 'Môi trường đã sẵn sàng', vmName: record.vmName, state: record.vmState, ip: record.ip,
        } : null);
        setEnvironmentPlan(null);
        setEnvironmentProgress(null);
        setEnvironmentResult(null);
        setCopyPlan(null);
        setCopyProgress(null);
        setCopyResult(null);
        setRuntimePlan(null);
        setRuntimeProgress(null);
        setRuntimeResult(null);
        setDatabasePlan(null);
        setDatabaseProgress(null);
        setDatabaseResult(null);
        setProjectDataStatus(null);
        setBackupProgress(null);
        setBackupResult(null);
        setTunnelDialogOpen(false);
        setTunnelPlan(null);
        setTunnelProgress(null);
        setTunnelResult(null);
        setWebsiteScreen('detail');
        setView('websites');
        void loadProjectData(record.path);
    }

    const openProjectSourceFolder = async () => {
        if (!selectedProject) return;
        try {
            await OpenProjectSource(selectedProject);
        } catch (error) {
            setNotice(`Không mở được source gốc: ${cleanError(error)}`);
        }
    };

    const createProjectBackup = async () => {
        if (!selectedProject || backingUp) return;
        setBackingUp(true);
        setBackupResult(null);
        setBackupProgress({stage: 'prepare', message: 'Đang chuẩn bị backup…', percent: 2});
        try {
            const result = await CreateProjectBackup(selectedProject) as BackupResult;
            setBackupResult(result);
            setNotice(result.success ? 'Đã lưu source và database vào thư mục backup của ứng dụng.' : 'Chưa tạo được backup; website đang chạy không bị thay đổi.');
        } catch (error) {
            setBackupResult({success: false, message: 'Không tạo được backup', detail: cleanError(error)});
        } finally {
            setBackingUp(false);
            await refreshProjects();
            await loadProjectData(selectedProject);
        }
    };

    const openProjectBackupFolder = async () => {
        if (!selectedProject) return;
        try {
            await OpenProjectBackupFolder(selectedProject);
        } catch (error) {
            setNotice(`Không mở được thư mục backup: ${cleanError(error)}`);
        }
    };

    const handleCheckAction = async (check: ReadinessCheck) => {
        if (check.actionKind === 'retry') {
            void refresh();
            return;
        }
        if (check.actionKind === 'install_multipass' || check.actionKind === 'repair_multipass' || check.actionKind === 'install_virtualbox') {
            try {
                const plan = await GetDependencyPlan(check.actionKind) as DependencyPlan;
                if (!plan.supported) {
                    setNotice(plan.description || 'Thao tác này chưa được hỗ trợ.');
                    return;
                }
                setInstallProgress(null);
                setInstallResult(null);
                setInstallPlan(plan);
            } catch (error) {
                setNotice(`Không chuẩn bị được trình cài đặt: ${String(error)}`);
            }
            return;
        }
        if (check.actionKind === 'free_disk') {
            setNotice('Giải phóng ít nhất 15 GB trên ổ đang chạy ứng dụng rồi kiểm tra lại.');
        } else if (check.actionKind === 'enable_virtualization') {
            setNotice('Bật Intel VT-x hoặc AMD-V trong BIOS/UEFI, khởi động lại máy rồi kiểm tra lại.');
        }
    };

    const startInstall = async () => {
        if (!installPlan || installing) return;
        setInstalling(true);
        setInstallResult(null);
        setInstallProgress({stage: 'start', message: 'Đang chuẩn bị…', percent: 1});
        try {
            const result = await RunDependencyAction(installPlan.action) as InstallerResult;
            setInstallResult(result);
            if (result.success && !result.rebootRequired) await refresh();
            if (result.success && result.rebootRequired) setNotice('Khởi động lại Windows trước khi tiếp tục tạo môi trường.');
        } catch (error) {
            setInstallResult({success: false, rebootRequired: false, message: 'Cài đặt không thành công', detail: String(error)});
        } finally {
            setInstalling(false);
        }
    };

    function closeInstallDialog() {
        if (installing) return;
        setInstallPlan(null);
        setInstallProgress(null);
        setInstallResult(null);
    }

    const prepareEnvironment = async () => {
        if (!projectInfo || !projectSettings || environmentPlanning) return;
        setEnvironmentPlanning(true);
        setEnvironmentProgress(null);
        setEnvironmentResult(null);
        try {
            const plan = await GetEnvironmentPlan({projectPath: projectInfo.path, projectName: projectSettings.name}) as EnvironmentPlan;
            setEnvironmentPlan(plan);
        } catch (error) {
            setNotice(`Không chuẩn bị được môi trường: ${String(error)}`);
        } finally {
            setEnvironmentPlanning(false);
        }
    };

    const executeEnvironment = async (recreate: boolean) => {
        if (!environmentPlan || !projectInfo || !projectSettings || creatingEnvironment) return;
        setCreatingEnvironment(true);
        setEnvironmentResult(null);
        setEnvironmentProgress({stage: recreate ? 'reset' : 'start', message: recreate ? 'Đang chuẩn bị tạo lại…' : 'Đang chuẩn bị…', percent: 1});
        try {
            const request = {projectPath: projectInfo.path, projectName: projectSettings.name};
            const result = await (recreate ? RecreateEnvironment(request) : CreateEnvironment(request)) as EnvironmentResult;
            setEnvironmentResult(result);
            if (result.success) {
                setEnvironmentInfo(result);
                setNotice('Môi trường an toàn đã sẵn sàng. Website chưa được sao chép ở bước này.');
            }
            await refreshProjects();
        } catch (error) {
            setEnvironmentResult({success: false, reused: false, message: 'Tạo môi trường không thành công', detail: String(error)});
            await refreshProjects();
        } finally {
            setCreatingEnvironment(false);
        }
    };

    const startEnvironment = async () => executeEnvironment(false);
    const recreateEnvironment = async () => executeEnvironment(true);

    function closeEnvironmentDialog() {
        if (creatingEnvironment) return;
        setEnvironmentPlan(null);
        setEnvironmentProgress(null);
        setEnvironmentResult(null);
    }

    const prepareCopy = async () => {
        if (!projectInfo || copyPlanning) return;
        setCopyPlanning(true);
        setCopyProgress(null);
        setCopyResult(null);
        try {
            setCopyPlan(await GetCopyPlan({projectPath: projectInfo.path}) as CopyPlan);
        } catch (error) {
            setNotice(`Không chuẩn bị được danh sách sao chép: ${cleanError(error)}`);
        } finally {
            setCopyPlanning(false);
        }
    };

    const executeCopy = async () => {
        if (!copyPlan || !projectInfo || copying) return;
        setCopying(true);
        setCopyResult(null);
        setCopyProgress({stage: 'scan', message: 'Đang kiểm tra danh sách file…', percent: 2});
        try {
            const result = await CopyWebsite({projectPath: projectInfo.path}) as CopyResult;
            setCopyResult(result);
            setNotice(result.success ? 'Website đã được sao chép vào máy ảo. Source trên máy không bị thay đổi.' : 'Sao chép chưa hoàn tất; chưa có website nào được chạy hoặc public.');
        } catch (error) {
            setCopyResult({success: false, message: 'Sao chép website không thành công', detail: cleanError(error)});
        } finally {
            setCopying(false);
            await refreshProjects();
        }
    };

    function closeCopyDialog() {
        if (copying) return;
        setCopyPlan(null);
        setCopyProgress(null);
        setCopyResult(null);
    }

    const prepareRuntime = async () => {
        if (!projectInfo || runtimePlanning) return;
        setRuntimePlanning(true);
        setRuntimeProgress(null);
        setRuntimeResult(null);
        try {
            setRuntimePlan(await GetRuntimePlan({projectPath: projectInfo.path}) as RuntimePlan);
        } catch (error) {
            setNotice(`Không chuẩn bị được môi trường chạy: ${cleanError(error)}`);
        } finally {
            setRuntimePlanning(false);
        }
    };

    const executeRuntime = async () => {
        if (!runtimePlan || !projectInfo || installingRuntime) return;
        setInstallingRuntime(true);
        setRuntimeResult(null);
        setRuntimeProgress({stage: 'verify', message: 'Đang kiểm tra snapshot và máy ảo…', percent: 2});
        try {
            const result = await InstallRuntime({projectPath: projectInfo.path}) as RuntimeResult;
            setRuntimeResult(result);
            setNotice(result.success ? 'Môi trường chạy đã sẵn sàng trong mạng nội bộ; website chưa được public.' : 'Cài môi trường chạy chưa hoàn tất; website chưa được public.');
        } catch (error) {
            setRuntimeResult({success: false, message: 'Cài môi trường chạy không thành công', detail: cleanError(error)});
        } finally {
            setInstallingRuntime(false);
            await refreshProjects();
        }
    };

    function closeRuntimeDialog() {
        if (installingRuntime) return;
        setRuntimePlan(null);
        setRuntimeProgress(null);
        setRuntimeResult(null);
    }

    const loadDatabasePlan = async (mode: DatabaseMode, importPath = '') => {
        if (!projectInfo || databasePlanning) return;
        setDatabasePlanning(true);
        setDatabaseProgress(null);
        setDatabaseResult(null);
        try {
            const plan = await GetDatabasePlan({projectPath: projectInfo.path, mode, importPath}) as DatabasePlan;
            setDatabaseMode(mode);
            setDatabaseImportPath(importPath);
            setDatabasePlan(plan);
        } catch (error) {
            setNotice(`Không chuẩn bị được database: ${cleanError(error)}`);
        } finally {
            setDatabasePlanning(false);
        }
    };

    const prepareDatabase = async () => loadDatabasePlan('automatic');

    const chooseDatabaseFile = async () => {
        if (!projectInfo || databasePlanning || importingDatabase) return;
        setDatabasePlanning(true);
        try {
            const path = await SelectDatabaseFile(projectInfo.path);
            if (!path) return;
            const plan = await GetDatabasePlan({projectPath: projectInfo.path, mode: 'file', importPath: path}) as DatabasePlan;
            setDatabaseMode('file');
            setDatabaseImportPath(path);
            setDatabasePlan(plan);
            setDatabaseProgress(null);
            setDatabaseResult(null);
        } catch (error) {
            setNotice(`Không đọc được file database: ${cleanError(error)}`);
        } finally {
            setDatabasePlanning(false);
        }
    };

    const executeDatabase = async () => {
        if (!databasePlan?.ready || !projectInfo || importingDatabase) return;
        setImportingDatabase(true);
        setDatabaseResult(null);
        setDatabaseProgress({stage: 'prepare', message: 'Đang chuẩn bị bản sao database…', percent: 2});
        try {
            const result = await ImportDatabase({projectPath: projectInfo.path, mode: databaseMode, importPath: databaseImportPath}) as DatabaseResult;
            setDatabaseResult(result);
            setNotice(result.success ? 'Source và database đã được sao chép riêng vào máy ảo; website chưa public.' : 'Database chưa được nhập; runtime và source vẫn được giữ nguyên.');
        } catch (error) {
            setDatabaseResult({success: false, message: 'Sao chép database không thành công', detail: cleanError(error)});
        } finally {
            setImportingDatabase(false);
            await refreshProjects();
        }
    };

    function closeDatabaseDialog() {
        if (importingDatabase) return;
        setDatabasePlan(null);
        setDatabaseMode('automatic');
        setDatabaseImportPath('');
        setDatabaseProgress(null);
        setDatabaseResult(null);
    }

    const openTunnelDialog = () => {
        if (!projectInfo) return;
        if (readyCloudflareDomains.length === 0) {
            setView('settings');
            setNotice('Thêm ít nhất một domain Cloudflare sẵn sàng trước khi xuất bản.');
            return;
        }
        const activeRecord = projects.find(item => samePath(item.path, projectInfo.path));
        const selectedDomain = readyCloudflareDomains.find(domain => domain.zone === activeRecord?.domainZone) || readyCloudflareDomains[0];
        const label = slugify(projectInfo.name) || `site-${Date.now().toString().slice(-5)}`;
        setTunnelZone(selectedDomain.zone);
        setTunnelSubdomain(label);
        setTunnelPlan(null);
        setTunnelProgress(null);
        setTunnelResult(null);
        setTunnelDialogOpen(true);
    };

    const prepareTunnel = async () => {
        if (!projectInfo || tunnelPlanning) return;
        setTunnelPlanning(true);
        setTunnelResult(null);
        try {
            const hostname = `${tunnelSubdomain.trim()}.${tunnelZone}`;
            setTunnelPlan(await GetTunnelPlan({projectPath: projectInfo.path, hostname}) as TunnelPlan);
        } catch (error) {
            setTunnelResult({success: false, message: 'Chưa thể dùng subdomain này', detail: cleanError(error)});
        } finally {
            setTunnelPlanning(false);
        }
    };

    const executeTunnel = async () => {
        if (!projectInfo || !tunnelPlan || startingTunnel) return;
        setStartingTunnel(true);
        setTunnelResult(null);
        setTunnelProgress({stage: 'cloudflare', message: 'Đang tạo tunnel và DNS trên Cloudflare…', percent: 3});
        try {
            const result = await StartNamedTunnel({projectPath: projectInfo.path, hostname: tunnelPlan.hostname}) as TunnelResult;
            setTunnelResult(result);
            setNotice(result.success ? `Website đã public tại ${result.url}.` : 'Chưa public domain; runtime và source vẫn được giữ nguyên.');
        } catch (error) {
            setTunnelResult({success: false, message: 'Không kết nối được domain', detail: cleanError(error)});
        } finally {
            setStartingTunnel(false);
            await refreshProjects();
        }
    };

    const stopTunnel = async () => {
        if (!projectInfo || stoppingTunnel) return;
        setStoppingTunnel(true);
        try {
            const result = await StopNamedTunnel({projectPath: projectInfo.path, hostname: ''}) as TunnelResult;
            setNotice(result.success ? 'Đã dừng truy cập domain; runtime vẫn được giữ lại.' : `${result.message}: ${result.detail || 'hãy thử lại'}`);
        } catch (error) {
            setNotice(`Không dừng được domain: ${cleanError(error)}`);
        } finally {
            setStoppingTunnel(false);
            await refreshProjects();
        }
    };

    const openPublicURL = async (url: string) => {
        try {
            await OpenExternalURL(url);
        } catch (error) {
            setNotice(`Không mở được website: ${cleanError(error)}`);
        }
    };

    function closeTunnelDialog() {
        if (startingTunnel) return;
        setTunnelDialogOpen(false);
        setTunnelPlan(null);
        setTunnelProgress(null);
        setTunnelResult(null);
    }

    const websiteTitle = websiteScreen === 'configure' ? 'Cấu hình website' : websiteScreen === 'source' ? 'Quản lý source' : websiteScreen === 'detail' ? 'Chi tiết website' : 'Website';
    const websiteDescription = websiteScreen === 'configure' ? 'Kiểm tra thông tin trước khi tạo môi trường.' : websiteScreen === 'source' ? 'Xem và chỉnh sửa source gốc của website.' : websiteScreen === 'detail' ? 'Theo dõi và triển khai website đã chọn.' : 'Danh sách website đang quản lý.';

    return (
        <div className="app-shell">
            <aside className="sidebar" aria-label="Điều hướng chính">
                <div className="brand"><span className="brand-mark"><Icon name="shield" size={20}/></span><span><strong>OneClick</strong><small>Dev Server</small></span></div>
                <nav className="nav-list">
                    <button className={view === 'overview' ? 'nav-item active' : 'nav-item'} onClick={() => setView('overview')}><Icon name="home"/><span>Tổng quan</span></button>
                    <button className={view === 'websites' ? 'nav-item active' : 'nav-item'} onClick={() => { setView('websites'); setWebsiteScreen('list'); }}><Icon name="globe"/><span>Website</span>{projects.length + newDiscoveredProjects.length > 0 && <span className="nav-count">{projects.length + newDiscoveredProjects.length}</span>}</button>
                    <button className={view === 'domains' ? 'nav-item active' : 'nav-item'} onClick={() => setView('domains')}><Icon name="link"/><span>Domain</span>{cloudflareDomains.length > 0 && <span className="nav-count">{cloudflareDomains.length}</span>}</button>
                    <button className={view === 'fresh-wordpress' ? 'nav-item active' : 'nav-item'} onClick={() => setView('fresh-wordpress')}><Icon name="file"/><span>Cài mới WP</span></button>
                    <button className={view === 'recovery' ? 'nav-item active' : 'nav-item'} onClick={() => setView('recovery')}><Icon name="recovery"/><span>Khôi phục WP</span></button>
                    <button className={view === 'system' ? 'nav-item active' : 'nav-item'} onClick={() => setView('system')}><Icon name="shield"/><span>Kiểm tra máy</span>{report && !report.ready && <span className="nav-alert" aria-label="Cần xử lý"/>}</button>
                </nav>
                <div className="sidebar-footer">
                    <button className={view === 'settings' ? 'nav-item muted active' : 'nav-item muted'} onClick={() => setView('settings')}><Icon name="settings"/><span>Cài đặt</span></button>
                    <p>Phiên bản 0.1.0</p>
                </div>
            </aside>

            <main className="main-content">
                <header className="topbar">
                    <div><h1>{view === 'system' ? 'Kiểm tra máy' : view === 'settings' ? 'Cài đặt' : view === 'domains' ? 'Domain' : view === 'fresh-wordpress' ? 'Cài mới WordPress' : view === 'recovery' ? 'Khôi phục WordPress' : view === 'websites' ? websiteTitle : 'Tổng quan'}</h1><p>{view === 'system' ? 'Các thành phần cần thiết trước khi xuất bản.' : view === 'settings' ? 'Dữ liệu và cấu hình hệ thống.' : view === 'domains' ? 'Quản lý domain dùng để xuất bản website.' : view === 'fresh-wordpress' ? 'Tạo website sạch với bộ theme và plugin có sẵn.' : view === 'recovery' ? 'Sao lưu, kiểm tra và dựng lại website an toàn.' : view === 'websites' ? websiteDescription : 'Tình trạng hoạt động hiện tại.'}</p></div>
                    <div className={report?.ready ? 'global-status ready' : 'global-status pending'}><span/>{report?.ready ? 'Sẵn sàng' : 'Cần thiết lập'}</div>
                </header>

                {notice && <div className="notice" role="status"><Icon name="info"/><span>{notice}</span><button aria-label="Đóng thông báo" onClick={() => setNotice('')}><Icon name="close" size={18}/></button></div>}
                {view === 'system' && <SystemView report={report} loading={loading} progress={progress} onRefresh={refresh} onContinue={chooseProject} onAction={handleCheckAction}/>}
                {view === 'overview' && <Overview report={report} projects={projects} loading={projectsLoading} onOpenProject={openStoredProject} onSetup={() => setView('system')} onChooseProject={chooseProject}/>}
                {view === 'domains' && <DomainsView domains={cloudflareDomains} loading={cloudflareLoading} onRefresh={refreshCloudflare}/>}
                {view === 'fresh-wordpress' && <RecoveryToolView mode="fresh"/>}
                {view === 'recovery' && <RecoveryToolView/>}
                {view === 'settings' && <SettingsView storage={dataStorageStatus} storageLoading={dataStorageLoading} onStorageRefresh={refreshDataStorage}/>}
                {view === 'websites' && <Websites report={report} projects={projects} projectsLoading={projectsLoading} discoveredProjects={newDiscoveredProjects} discoveryLoading={discoveryLoading} storagePath={storagePath} selectedProject={selectedProject} projectInfo={projectInfo} settings={projectSettings} configured={projectConfigured} environment={environmentInfo} environmentPlanning={environmentPlanning} copyPlanning={copyPlanning} runtimePlanning={runtimePlanning} databasePlanning={databasePlanning} tunnelPlanning={tunnelPlanning} stoppingTunnel={stoppingTunnel} projectData={projectDataStatus} projectDataLoading={projectDataLoading} backupProgress={backupProgress} backupResult={backupResult} backingUp={backingUp} cloudflareDomains={cloudflareDomains} loading={projectLoading} screen={websiteScreen} onOpenProject={openStoredProject} onOpenDiscovered={openDetectedProject} onRefreshDiscovery={refreshDiscovery} onScreenChange={setWebsiteScreen} onSettingsChange={setProjectSettings} onConfirmSettings={confirmProjectSettings} onCreateEnvironment={prepareEnvironment} onCopyWebsite={prepareCopy} onInstallRuntime={prepareRuntime} onImportDatabase={prepareDatabase} onStartTunnel={openTunnelDialog} onStopTunnel={stopTunnel} onOpenURL={openPublicURL} onOpenSource={() => setWebsiteScreen('source')} onOpenSourceFolder={openProjectSourceFolder} onCreateBackup={createProjectBackup} onOpenBackup={openProjectBackupFolder} onCloudflareSetup={() => setView('domains')} onSetup={() => setView('system')} onChooseProject={chooseProject}/>}
            </main>

            {installPlan && <InstallDialog plan={installPlan} progress={installProgress} result={installResult} installing={installing} onConfirm={startInstall} onClose={closeInstallDialog}/>}
            {environmentPlan && <EnvironmentDialog plan={environmentPlan} progress={environmentProgress} result={environmentResult} creating={creatingEnvironment} onConfirm={startEnvironment} onRecreate={recreateEnvironment} onClose={closeEnvironmentDialog}/>}
            {copyPlan && <CopyDialog plan={copyPlan} progress={copyProgress} result={copyResult} copying={copying} onConfirm={executeCopy} onClose={closeCopyDialog}/>}
            {runtimePlan && <RuntimeDialog plan={runtimePlan} progress={runtimeProgress} result={runtimeResult} installing={installingRuntime} onConfirm={executeRuntime} onClose={closeRuntimeDialog}/>}
            {databasePlan && <DatabaseDialog plan={databasePlan} mode={databaseMode} progress={databaseProgress} result={databaseResult} planning={databasePlanning} importing={importingDatabase} onUseAutomatic={() => void loadDatabasePlan('automatic')} onChooseFile={chooseDatabaseFile} onConfirm={executeDatabase} onClose={closeDatabaseDialog}/>}
            {tunnelDialogOpen && <TunnelDialog domains={readyCloudflareDomains} subdomain={tunnelSubdomain} zone={tunnelZone} plan={tunnelPlan} progress={tunnelProgress} result={tunnelResult} planning={tunnelPlanning} starting={startingTunnel} onSubdomainChange={value => { setTunnelSubdomain(value); setTunnelPlan(null); setTunnelResult(null); }} onZoneChange={value => { setTunnelZone(value); setTunnelPlan(null); setTunnelResult(null); }} onPlan={prepareTunnel} onConfirm={executeTunnel} onOpen={openPublicURL} onClose={closeTunnelDialog}/>}
        </div>
    );
}

function EnvironmentDialog({plan, progress, result, creating, onConfirm, onRecreate, onClose}: {
    plan: EnvironmentPlan;
    progress: EnvironmentProgress | null;
    result: EnvironmentResult | null;
    creating: boolean;
    onConfirm: () => Promise<void>;
    onRecreate: () => Promise<void>;
    onClose: () => void;
}) {
    const [confirmRecreate, setConfirmRecreate] = useState(false);
    const dialogRef = useDialogFocus(onClose, !creating);
    return <div className="modal-backdrop" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}>
        <section ref={dialogRef} tabIndex={-1} className="install-dialog environment-dialog" role="dialog" aria-modal="true" aria-labelledby="environment-title" aria-busy={creating}>
            <header className="dialog-header">
                <div><h2 id="environment-title">Chuẩn bị máy chủ Ubuntu</h2><p>Ubuntu dùng chung; dữ liệu từng website vẫn tách riêng.</p></div>
                <button className="icon-button" aria-label="Đóng" disabled={creating} onClick={onClose}><Icon name="close" size={18}/></button>
            </header>
            {!progress && !result && <>
                <dl className="plan-details environment-plan">
                    <div><dt>Tên máy ảo</dt><dd>{plan.vmName}</dd></div>
                    <div><dt>Hệ điều hành</dt><dd>{plan.image}</dd></div>
                    <div><dt>Tài nguyên</dt><dd>{plan.cpus} CPU · {plan.memory} RAM · {plan.disk} ổ đĩa</dd></div>
                    <div><dt>Mạng</dt><dd>{plan.network}</dd></div>
                    <div><dt>Cách ly</dt><dd>{plan.isolation}</dd></div>
                </dl>
                <div className="security-note"><Icon name="shield" size={17}/><span>{plan.existing ? 'Máy chủ Ubuntu OneClick đã có sẽ được kiểm tra và dùng chung.' : 'Ubuntu chỉ được tạo một lần; chưa sao chép hoặc chạy website ở bước này.'}</span></div>
            </>}
            {progress && !result && <div className="install-progress" aria-live="polite">
                <div className="progress-line"><span style={{width: `${progress.percent}%`}}/></div>
                <div><strong>{progress.message}</strong><span>{progress.percent}%</span></div>
                <p>Có thể tiếp tục làm việc; cửa sổ dòng lệnh sẽ không xuất hiện.</p>
            </div>}
            {result && <div className={result.success ? 'install-result success' : 'install-result error'} role={result.success ? 'status' : 'alert'}>
                <span><Icon name={result.success ? 'check' : 'close'} size={22}/></span>
                <div><strong>{result.message}</strong>{result.success && <p>{result.vmName}{result.ip ? ` · ${result.ip}` : ''}</p>}{!result.success && <p>{result.rebootRequired ? 'Khởi động lại Windows rồi mở OneClick để tiếp tục.' : result.canRecreate ? 'Máy chủ chưa chứa website có thể được tạo lại sau xác nhận.' : 'Không có website nào được public. Kiểm tra máy rồi thử lại.'}</p>}{result.detail && (result.rebootRequired ? <p>{shortText(result.detail, 600)}</p> : <details className="technical-error"><summary>Chi tiết kỹ thuật</summary><pre>{shortText(result.detail, 1200)}</pre></details>)}</div>
            </div>}
            {result?.canRecreate && confirmRecreate && <div className="recreate-confirm" role="alert"><Icon name="warning" size={18}/><div><strong>Xoá máy chủ Ubuntu lỗi và tạo lại?</strong><p>OneClick chỉ cho phép khi máy chủ chưa chứa snapshot, database hoặc tunnel của website nào.</p></div></div>}
            <footer className="dialog-actions">
                {!result && <button className="button secondary" disabled={creating} onClick={onClose}>Huỷ</button>}
                {!result && <button className="button primary" disabled={creating} autoFocus onClick={() => void onConfirm()}>{creating ? 'Đang tạo…' : (plan.existing ? 'Kiểm tra và dùng lại' : 'Chuẩn bị Ubuntu')}</button>}
                {result && !result.success && !confirmRecreate && !result.rebootRequired && <button className="button secondary" onClick={onClose}>Đóng</button>}
                {result && !result.success && !result.canRecreate && !result.rebootRequired && <button className="button primary" onClick={() => void onConfirm()}>Thử lại</button>}
                {result?.rebootRequired && <button className="button primary" onClick={onClose}>Đã hiểu</button>}
                {result?.canRecreate && !confirmRecreate && <button className="button primary" onClick={() => setConfirmRecreate(true)}>Tạo lại từ đầu</button>}
                {result?.canRecreate && confirmRecreate && <button className="button secondary" onClick={() => setConfirmRecreate(false)}>Quay lại</button>}
                {result?.canRecreate && confirmRecreate && <button className="button danger" onClick={() => { setConfirmRecreate(false); void onRecreate(); }}>Xoá và tạo lại</button>}
                {result?.success && <button className="button primary" onClick={onClose}>Hoàn tất</button>}
            </footer>
        </section>
    </div>;
}

function RecoveryToolView({mode = 'recovery'}: {mode?: 'recovery' | 'fresh'}) {
    const [status, setStatus] = useState<RecoveryToolStatus | null>(null);
    const [progress, setProgress] = useState<RecoveryToolProgress | null>(null);
    const [loading, setLoading] = useState(true);
    const [preparing, setPreparing] = useState(false);
    const [frameLoaded, setFrameLoaded] = useState(false);
    const started = useRef(false);
    const runtimeInstalled = status?.state === 'start_required';
    const isFresh = mode === 'fresh';
    const toolName = isFresh ? 'Cài mới WordPress' : 'Khôi phục WP';

    useEffect(() => EventsOn('recovery-tool:progress', (value: RecoveryToolProgress) => setProgress(value)), []);

    const start = useCallback(async () => {
        setLoading(true);
        setFrameLoaded(false);
        try {
            const current = await GetRecoveryToolStatus() as RecoveryToolStatus;
            if (!current.ready) {
                setStatus(current);
                return;
            }
            setStatus(await StartRecoveryTool() as RecoveryToolStatus);
        } catch (error) {
            setStatus({state: 'error', message: `Không mở được ${toolName}`, detail: cleanError(error), ready: false, running: false, setupRequired: false});
        } finally {
            setLoading(false);
        }
    }, [toolName]);

    useEffect(() => {
        if (started.current) return;
        started.current = true;
        void start();
    }, [start]);

    const prepare = async () => {
        setPreparing(true);
        setProgress({stage: 'bundle', message: runtimeInstalled ? 'Đang khởi động môi trường Khôi phục WP…' : 'Đang chuẩn bị mã nguồn trong OneClick…', percent: 5});
        try {
            const prepared = await PrepareRecoveryTool() as RecoveryToolStatus;
            setStatus(prepared);
            if (prepared.ready) await start();
        } catch (error) {
            setStatus({state: 'error', message: `Không chuẩn bị được ${toolName}`, detail: cleanError(error), ready: false, running: false, setupRequired: true});
        } finally {
            setPreparing(false);
            setProgress(null);
        }
    };

    if (status?.running && status.url) {
        return <section className="recovery-tool-page" aria-busy={!frameLoaded}>
            {!frameLoaded && <div className="recovery-tool-loading" role="status"><span className="recovery-tool-spinner"/><div><strong>Đang mở {toolName}…</strong><p>Giao diện và engine đang chạy nội bộ trong OneClick.</p></div></div>}
            <iframe
                className={frameLoaded ? 'recovery-tool-frame ready' : 'recovery-tool-frame'}
                src={isFresh ? `${status.url}fresh` : status.url}
                title={toolName}
                sandbox="allow-scripts allow-forms allow-downloads allow-same-origin allow-popups allow-modals"
                referrerPolicy="no-referrer"
                onLoad={() => setFrameLoaded(true)}
            />
        </section>;
    }

    return <section className="content-stack recovery-tool-setup" aria-busy={loading || preparing}>
        <section className="configuration-form">
            <header className="form-header"><div><h2>{toolName}</h2><p>{isFresh ? 'WordPress, Bricks và plugin đã được đóng gói sẵn trong OneClick.' : 'Mã nguồn đã nằm trong OneClick; không phụ thuộc thư mục dự án cũ.'}</p></div><span className={status?.setupRequired ? 'detected-badge warning' : status?.state === 'error' ? 'detected-badge danger' : 'detected-badge'}>{preparing ? (runtimeInstalled ? 'Đang khởi động' : 'Đang cài') : runtimeInstalled ? 'Sẵn sàng' : status?.setupRequired ? 'Cần chuẩn bị' : status?.state === 'error' ? 'Có lỗi' : 'Đang mở'}</span></header>
            <div className="recovery-tool-setup-body">
                {loading && !status ? <div className="storage-loading">Đang kiểm tra môi trường Khôi phục WP…</div> : <>
                    <div className={status?.state === 'error' ? 'inline-error' : 'inline-warning'} role={status?.state === 'error' ? 'alert' : 'status'}><Icon name={status?.state === 'error' ? 'close' : 'info'} size={17}/><div><strong>{status?.message || 'Đang chuẩn bị'}</strong>{status?.detail && <span>{status.detail}</span>}</div></div>
                    {status?.dataPath && <div className="recovery-tool-path"><span>Dữ liệu ứng dụng</span><code title={status.dataPath}>{status.dataPath}</code></div>}
                    {progress && <div className="storage-progress" aria-live="polite"><div className="progress-line"><span style={{width: `${progress.percent}%`}}/></div><div><strong>{progress.message}</strong><span>{progress.percent}%</span></div></div>}
                </>}
            </div>
            <footer className="form-actions"><span>{status?.version ? `Bundle ${status.version}` : 'Đang kiểm tra bundle…'}</span><div><button className="button secondary" disabled={loading || preparing} onClick={() => void start()}><Icon name="refresh" size={16}/>Kiểm tra lại</button>{status?.setupRequired && <button className="button primary" disabled={preparing} onClick={() => void prepare()}>{preparing ? (runtimeInstalled ? 'Đang khởi động…' : 'Đang cài…') : runtimeInstalled ? 'Khởi động môi trường' : 'Cài môi trường'}</button>}</div></footer>
        </section>
    </section>;
}

function SettingsView({storage, storageLoading, onStorageRefresh}: {
    storage: DataStorageStatus | null;
    storageLoading: boolean;
    onStorageRefresh: () => Promise<void>;
}) {
    const [selectedStorage, setSelectedStorage] = useState('');
    const [storageProgress, setStorageProgress] = useState<DataStorageProgress | null>(null);
    const [storageResult, setStorageResult] = useState<DataStorageResult | null>(null);
    const [storageError, setStorageError] = useState('');
    const [configuringStorage, setConfiguringStorage] = useState(false);
    const [legacyPlan, setLegacyPlan] = useState<LegacyImportPlan | null>(null);
    const [legacyProgress, setLegacyProgress] = useState<LegacyImportProgress | null>(null);
    const [legacyResult, setLegacyResult] = useState<LegacyImportResult | null>(null);
    const [legacyError, setLegacyError] = useState('');
    const [legacyPlanning, setLegacyPlanning] = useState(false);
    const [legacyImporting, setLegacyImporting] = useState(false);

    useEffect(() => EventsOn('storage:progress', (value: DataStorageProgress) => setStorageProgress(value)), []);
    useEffect(() => EventsOn('recovery-tool:import-progress', (value: LegacyImportProgress) => setLegacyProgress(value)), []);

    const chooseStorage = async () => {
        setStorageError('');
        setStorageResult(null);
        try {
            const path = await SelectDataStorageDirectory();
            if (path) setSelectedStorage(path);
        } catch (chooseError) {
            setStorageError(cleanError(chooseError));
        }
    };

    const configureStorage = async () => {
        if (!selectedStorage.trim()) return;
        setConfiguringStorage(true);
        setStorageError('');
        setStorageResult(null);
        setStorageProgress({stage: 'check', message: 'Đang kiểm tra thư mục…', percent: 3});
        try {
            const result = await ConfigureDataStorage(selectedStorage.trim()) as DataStorageResult;
            setStorageResult(result);
            if (result.success) {
                setSelectedStorage('');
                await onStorageRefresh();
            }
        } catch (configureError) {
            setStorageError(cleanError(configureError));
        } finally {
            setConfiguringStorage(false);
            setStorageProgress(null);
        }
    };

    const chooseLegacyData = async () => {
        setLegacyError('');
        setLegacyResult(null);
        setLegacyPlan(null);
        setLegacyPlanning(true);
        try {
            const path = await SelectLegacyRecoveryDirectory();
            if (!path) return;
            const plan = await GetLegacyRecoveryImportPlan(path) as LegacyImportPlan;
            setLegacyPlan(plan);
            if (!plan.ready && plan.detail) setLegacyError(plan.detail);
        } catch (error) {
            setLegacyError(cleanError(error));
        } finally {
            setLegacyPlanning(false);
        }
    };

    const importLegacyData = async () => {
        if (!legacyPlan?.ready) return;
        setLegacyImporting(true);
        setLegacyError('');
        setLegacyResult(null);
        setLegacyProgress({stage: 'copy', message: 'Đang bắt đầu sao chép…', percent: 1, filesDone: 0, filesTotal: legacyPlan.files, bytesDone: 0, bytesTotal: legacyPlan.bytes});
        try {
            const result = await ImportLegacyRecoveryData(legacyPlan.sourcePath) as LegacyImportResult;
            setLegacyResult(result);
            if (!result.success && result.detail) setLegacyError(result.detail);
        } catch (error) {
            setLegacyError(cleanError(error));
        } finally {
            setLegacyImporting(false);
            setLegacyProgress(null);
        }
    };

    return <section className="content-stack settings-page">
        <section className="configuration-form storage-form" aria-busy={configuringStorage || storageLoading}>
            <header className="form-header">
                <div><h2>Nơi lưu dữ liệu máy ảo</h2><p>Ảnh Ubuntu, snapshot và database sẽ nằm trên ổ đã chọn.</p></div>
                {storage && <span className={storage.configured ? 'detected-badge success' : storage.locked ? 'detected-badge warning' : 'detected-badge'}>{storage.configured ? 'Đã cố định' : storage.locked ? 'Đã khóa' : 'Chưa thiết lập'}</span>}
            </header>

            {storageLoading && !storage && <div className="storage-loading">Đang kiểm tra nơi lưu dữ liệu…</div>}
            {storage && <div className="storage-body">
                <div className="storage-location">
                    <span className="storage-folder-icon"><Icon name="folder" size={20}/></span>
                    <div><span>{storage.configured ? 'Đang sử dụng' : 'Hiện tại — mặc định Windows'}</span><code title={storage.currentPath}>{storage.currentPath || 'Chưa xác định'}</code></div>
                </div>
                <div className="storage-facts">
                    <div><span>Dung lượng trống</span><strong>{storage.freeBytes ? formatBytes(storage.freeBytes) : 'Chưa đọc được'}</strong></div>
                    <div><span>Dự án</span><strong>{formatNumber(storage.projectCount)}</strong></div>
                    <div><span>Máy ảo</span><strong>{formatNumber(storage.instanceCount)}</strong></div>
                </div>
                {!storage.configured && storage.recommendedPath && <div className="recommended-path"><span>Thư mục đề xuất</span><code title={storage.recommendedPath}>{storage.recommendedPath}</code></div>}
                {storageError && <div className="inline-error" role="alert"><Icon name="close" size={17}/><span>{storageError}</span></div>}
                {storageResult && <div className={storageResult.success ? 'inline-success' : 'inline-error'} role={storageResult.success ? 'status' : 'alert'}><Icon name={storageResult.success ? 'check' : 'close'} size={17}/><div><strong>{storageResult.message}</strong>{storageResult.detail && <span>{storageResult.detail}</span>}</div></div>}
                {storageProgress && <div className="storage-progress" aria-live="polite"><div className="progress-line"><span style={{width: `${storageProgress.percent}%`}}/></div><div><strong>{storageProgress.message}</strong><span>{storageProgress.percent}%</span></div></div>}
                {selectedStorage && !configuringStorage && <div className="storage-confirm">
                    <div><span>Thư mục sẽ dùng</span><code title={selectedStorage}>{selectedStorage}</code><p>Windows sẽ hỏi quyền quản trị một lần và khởi động lại dịch vụ Multipass.</p></div>
                    <div><button className="button secondary" onClick={() => setSelectedStorage('')}>Huỷ</button><button className="button primary" onClick={() => void configureStorage()}>Xác nhận thiết lập</button></div>
                </div>}
            </div>}

            <footer className="form-actions storage-actions">
                <span>{storage?.message || 'Đang kiểm tra…'}</span>
                <div>
                    <button className="button secondary" disabled={storageLoading || configuringStorage} onClick={() => void onStorageRefresh()}><Icon name="refresh" size={16}/>Kiểm tra lại</button>
                    {storage?.canConfigure && <button className="button secondary" disabled={configuringStorage} onClick={() => void chooseStorage()}>Chọn thư mục</button>}
                    {storage?.canConfigure && <button className="button primary" disabled={configuringStorage || !storage.recommendedPath} onClick={() => { setStorageError(''); setStorageResult(null); setSelectedStorage(storage.recommendedPath); }}>Dùng thư mục đề xuất</button>}
                </div>
            </footer>
        </section>

        <section className="configuration-form legacy-import-form" aria-busy={legacyPlanning || legacyImporting}>
            <header className="form-header">
                <div><h2>Nhập dữ liệu khôi phục WordPress</h2><p>Đưa dự án, backup và lịch sử từ bản đang chạy bên ngoài vào OneClick.</p></div>
                <span className={legacyResult?.success ? 'detected-badge success' : legacyPlan?.ready ? 'detected-badge' : 'detected-badge warning'}>{legacyResult?.success ? 'Đã nhập' : legacyImporting ? 'Đang sao chép' : legacyPlan?.ready ? 'Chờ xác nhận' : 'Chưa chọn'}</span>
            </header>
            <div className="storage-body legacy-import-body">
                {legacyPlanning && <div className="storage-loading">Đang quét số file và dung lượng…</div>}
                {legacyPlan && !legacyPlanning && <>
                    <div className="storage-location"><span className="storage-folder-icon"><Icon name="archive" size={20}/></span><div><span>Thư mục cũ</span><code title={legacyPlan.sourcePath}>{legacyPlan.sourcePath}</code></div></div>
                    <div className="storage-facts legacy-import-facts">
                        <div><span>Dữ liệu</span><strong>{formatBytes(legacyPlan.bytes)}</strong></div>
                        <div><span>Số file</span><strong>{formatNumber(legacyPlan.files)}</strong></div>
                        <div><span>Dự án</span><strong>{formatNumber(legacyPlan.profiles)}</strong></div>
                        <div><span>Đã có</span><strong>{formatNumber(legacyPlan.existingFiles)}</strong></div>
                    </div>
                    <div className="recovery-tool-path"><span>Lưu vào OneClick</span><code title={legacyPlan.destinationPath}>{legacyPlan.destinationPath}</code></div>
                    <div className="security-note"><Icon name="shield" size={17}/><span>Mật khẩu trong sites JSON sẽ chuyển vào kho bí mật Windows và bị loại khỏi bản JSON mới. Thư mục cũ không bị xóa.</span></div>
                </>}
                {legacyError && <div className="inline-error" role="alert"><Icon name="close" size={17}/><span>{legacyError}</span></div>}
                {legacyResult && <div className={legacyResult.success ? 'inline-success' : 'inline-error'} role={legacyResult.success ? 'status' : 'alert'}><Icon name={legacyResult.success ? 'check' : 'close'} size={17}/><div><strong>{legacyResult.message}</strong>{legacyResult.success && <span>{formatNumber(legacyResult.files)} file mới · {formatBytes(legacyResult.bytes)} · bỏ qua {formatNumber(legacyResult.skipped)} file đã có</span>}{legacyResult.detail && <span>{legacyResult.detail}</span>}</div></div>}
                {legacyProgress && <div className="storage-progress" aria-live="polite"><div className="progress-line"><span style={{width: `${legacyProgress.percent}%`}}/></div><div><strong>{legacyProgress.message}</strong><span>{legacyProgress.percent}%</span></div><div className="legacy-progress-meta"><span>{formatNumber(legacyProgress.filesDone)}/{formatNumber(legacyProgress.filesTotal)} file</span><span>{formatBytes(legacyProgress.bytesDone)} / {formatBytes(legacyProgress.bytesTotal)}</span></div></div>}
            </div>
            <footer className="form-actions storage-actions">
                <span>{legacyPlan?.message || 'Chọn đúng thư mục gốc có pyproject.toml và src/wpclean.'}</span>
                <div><button className="button secondary" disabled={legacyPlanning || legacyImporting} onClick={() => void chooseLegacyData()}><Icon name="folder" size={16}/>{legacyPlanning ? 'Đang quét…' : 'Chọn thư mục cũ'}</button>{legacyPlan?.ready && <button className="button primary" disabled={legacyImporting} onClick={() => void importLegacyData()}>{legacyImporting ? 'Đang sao chép…' : 'Xác nhận sao chép'}</button>}</div>
            </footer>
        </section>
    </section>;
}

function DomainsView({domains, loading, onRefresh}: {
    domains: CloudflareStatus[];
    loading: boolean;
    onRefresh: () => Promise<void>;
}) {
    const [zone, setZone] = useState('');
    const [token, setToken] = useState('');
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');
    const [removingZone, setRemovingZone] = useState('');
    const [confirmRemoveZone, setConfirmRemoveZone] = useState('');

    const save = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        if (!zone.trim() || !token.trim()) {
            setError('Nhập domain và API token Cloudflare.');
            return;
        }
        setSaving(true);
        setError('');
        try {
            await SaveCloudflareConnection({zone: zone.trim(), apiToken: token.trim()}) as CloudflareStatus;
            setToken('');
            setZone('');
            await onRefresh();
        } catch (saveError) {
            setError(cleanError(saveError));
        } finally {
            setSaving(false);
        }
    };

    const openCloudflare = async () => {
        try { await OpenExternalURL('https://dash.cloudflare.com/'); }
        catch (openError) { setError(cleanError(openError)); }
    };

    const editDomain = (domain: CloudflareStatus) => {
        setZone(domain.zone);
        setToken('');
        setError('');
        setConfirmRemoveZone('');
    };

    const removeDomain = async (domain: CloudflareStatus) => {
        setRemovingZone(domain.zone);
        setError('');
        try {
            await RemoveCloudflareConnection(domain.zone);
            setConfirmRemoveZone('');
            if (zone === domain.zone) setZone('');
            await onRefresh();
        } catch (removeError) {
            setError(cleanError(removeError));
        } finally {
            setRemovingZone('');
        }
    };

    return <section className="content-stack domains-page">
        <section className="configuration-form domain-manager" aria-busy={loading}>
            <header className="form-header"><div><h2>Danh sách domain</h2><p>Mỗi website có thể chọn một domain riêng khi xuất bản.</p></div><div className="domain-header-actions"><span className="detected-badge">{domains.length} domain</span><button className="button secondary" disabled={loading} onClick={() => void onRefresh()}><Icon name="refresh" size={16}/>{loading ? 'Đang kiểm tra' : 'Kiểm tra lại'}</button></div></header>
            {domains.length === 0 && !loading ? <div className="domain-empty"><span className="empty-icon"><Icon name="link" size={24}/></span><div><strong>Chưa có domain</strong><p>Thêm domain Cloudflare ở biểu mẫu bên dưới.</p></div></div> : <div className="domain-list" aria-live="polite">
                {domains.map(domain => <article className="domain-row" key={domain.zone}>
                    <span className={domain.dnsReady && domain.tokenStored ? 'domain-status-icon ready' : 'domain-status-icon warning'}><Icon name={domain.dnsReady && domain.tokenStored ? 'check' : 'warning'} size={18}/></span>
                    <div className="domain-identity"><strong>{domain.zone}</strong><span>{domain.message}</span></div>
                    <span className={domain.dnsReady && domain.tokenStored ? 'project-status ready' : 'project-status running'}>{domain.dnsReady && domain.tokenStored ? 'Sẵn sàng' : domain.tokenStored ? 'Chờ DNS' : 'Cần token'}</span>
                    <div className="domain-row-actions">
                        {confirmRemoveZone === domain.zone ? <><button className="button tertiary" disabled={removingZone === domain.zone} onClick={() => setConfirmRemoveZone('')}>Huỷ</button><button className="button danger" disabled={removingZone === domain.zone} onClick={() => void removeDomain(domain)}>{removingZone === domain.zone ? 'Đang xóa…' : 'Xác nhận xóa'}</button></> : <><button className="button tertiary" onClick={() => editDomain(domain)}>Cập nhật token</button><button className="button secondary" onClick={() => setConfirmRemoveZone(domain.zone)}>Xóa</button></>}
                    </div>
                    {!domain.dnsReady && domain.tokenStored && <details className="domain-dns-detail"><summary>Xem nameserver</summary><div className="dns-columns"><div><strong>Nameserver cần dùng</strong>{(domain.assignedNameServers || []).map(value => <code key={value}>{value}</code>)}</div><div><strong>Đang hoạt động</strong>{(domain.activeNameServers || []).map(value => <code key={value}>{value}</code>)}</div></div></details>}
                </article>)}
            </div>}
        </section>

        <form className="configuration-form cloudflare-form" onSubmit={event => void save(event)}>
            <header className="form-header"><div><h2>{domains.some(domain => domain.zone === zone.trim().toLowerCase()) ? 'Cập nhật domain' : 'Thêm domain'}</h2><p>Token được lưu riêng trong kho bí mật của hệ điều hành.</p></div>{zone && domains.some(domain => domain.zone === zone.trim().toLowerCase()) && <span className="detected-badge">Đã có trong danh sách</span>}</header>
            {error && <div className="inline-error" role="alert"><Icon name="close" size={17}/><span>{error}</span></div>}
            <div className="form-grid">
                <label className="field"><span>Domain gốc</span><input value={zone} onChange={event => { setZone(event.target.value.toLowerCase()); setError(''); }} onBlur={() => setZone(value => value.trim().replace(/\.$/, ''))} placeholder="example.com" autoComplete="off"/><small>Domain phải được thêm vào tài khoản Cloudflare trước.</small></label>
                <label className="field"><span>Cloudflare API token</span><input type="password" value={token} onChange={event => { setToken(event.target.value); setError(''); }} placeholder="Dán token của domain này" autoComplete="off"/><small>Quyền tối thiểu: Zone Read, DNS Write và Cloudflare Tunnel Write.</small></label>
            </div>
            <footer className="form-actions"><button type="button" className="button secondary" onClick={() => void openCloudflare()}>Mở Cloudflare</button><div><span>Token không được ghi vào state.json.</span><button className="button primary" type="submit" disabled={saving || !zone.trim() || !token.trim()}>{saving ? 'Đang xác minh…' : domains.some(domain => domain.zone === zone.trim().toLowerCase()) ? 'Cập nhật domain' : 'Thêm domain'}</button></div></footer>
        </form>
    </section>;
}

function RecoveryView({snapshot, loading, onRefresh}: {
    snapshot: RecoverySnapshot;
    loading: boolean;
    onRefresh: () => Promise<void>;
}) {
    const emptyForm = (): RecoveryProjectInput => ({id: '', name: '', host: '', username: '', password: '', protocol: 'ftps', port: 21, remotePath: '', siteUrl: '', workers: 4, passive: true, allowInsecure: false});
    const [form, setForm] = useState<RecoveryProjectInput>(emptyForm);
    const [saving, setSaving] = useState(false);
    const [testingID, setTestingID] = useState('');
    const [openingID, setOpeningID] = useState('');
    const [removingID, setRemovingID] = useState('');
    const [confirmRemoveID, setConfirmRemoveID] = useState('');
    const [error, setError] = useState('');
    const [results, setResults] = useState<Record<string, RecoveryConnectionResult>>({});
    const [backupPlan, setRecoveryBackupPlan] = useState<RecoveryBackupPlan | null>(null);
    const [backupProgress, setRecoveryBackupProgress] = useState<RecoveryBackupProgress | null>(null);
    const [backupResult, setRecoveryBackupResult] = useState<RecoveryBackupResult | null>(null);
    const [backupPlanningID, setBackupPlanningID] = useState('');
    const [backingUp, setBackingUp] = useState(false);
    const formRef = useRef<HTMLFormElement>(null);

    useEffect(() => EventsOn('recovery:backup-progress', (value: RecoveryBackupProgress) => setRecoveryBackupProgress(value)), []);

    const update = <Key extends keyof RecoveryProjectInput>(key: Key, value: RecoveryProjectInput[Key]) => {
        setForm(current => ({...current, [key]: value}));
        setError('');
    };

    const updateHost = (value: string) => {
        setForm(current => {
            const previousSuggestion = suggestedRecoveryPath(current.host);
            const pathWasAutomatic = !current.remotePath || current.remotePath === previousSuggestion || current.remotePath === '/public_html';
            return {...current, host: value, remotePath: pathWasAutomatic ? suggestedRecoveryPath(value) : current.remotePath};
        });
        setError('');
    };

    const startNew = () => {
        setForm(emptyForm());
        setError('');
        setConfirmRemoveID('');
        window.requestAnimationFrame(() => formRef.current?.scrollIntoView({behavior: 'smooth', block: 'start'}));
    };

    const edit = (project: RecoveryProject) => {
        setForm({
            id: project.id, name: project.name, host: project.host, username: project.username,
            password: '', protocol: project.protocol, port: project.port, remotePath: project.remotePath,
            siteUrl: project.siteUrl || '', workers: project.workers, passive: true,
            allowInsecure: project.protocol === 'ftp',
        });
        setError('');
        setConfirmRemoveID('');
        window.requestAnimationFrame(() => formRef.current?.scrollIntoView({behavior: 'smooth', block: 'start'}));
    };

    const testConnection = async (projectID: string) => {
        if (!projectID || testingID) return;
        setTestingID(projectID);
        setError('');
        try {
            const result = await TestRecoveryConnection(projectID) as RecoveryConnectionResult;
            setResults(current => ({...current, [projectID]: result}));
            await onRefresh();
        } catch (testError) {
            setResults(current => ({...current, [projectID]: {success: false, message: 'Không kiểm tra được kết nối', detail: cleanError(testError), secure: false}}));
        } finally {
            setTestingID('');
        }
    };

    const saveAndTest = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        if (form.protocol === 'ftp' && !form.allowInsecure) {
            setError('FTP không mã hóa. Hãy xác nhận rủi ro hoặc chọn FTPS.');
            return;
        }
        if (!form.password && !form.id) {
            setError('Nhập mật khẩu FTP/FTPS cho website mới.');
            return;
        }
        setSaving(true);
        setError('');
        try {
            const saved = await SaveRecoveryProject({...form, port: Number(form.port), workers: Number(form.workers)}) as RecoveryProject;
            setForm(current => ({...current, id: saved.id, password: ''}));
            await onRefresh();
            await testConnection(saved.id);
        } catch (saveError) {
            setError(cleanError(saveError));
        } finally {
            setSaving(false);
        }
    };

    const openFolder = async (projectID: string) => {
        setOpeningID(projectID);
        setError('');
        try {
            await OpenRecoveryFolder(projectID);
        } catch (openError) {
            setError(cleanError(openError));
        } finally {
            setOpeningID('');
        }
    };

    const remove = async (projectID: string) => {
        setRemovingID(projectID);
        setError('');
        try {
            await RemoveRecoveryProject(projectID);
            setConfirmRemoveID('');
            if (form.id === projectID) setForm(emptyForm());
            await onRefresh();
        } catch (removeError) {
            setError(cleanError(removeError));
        } finally {
            setRemovingID('');
        }
    };

    const prepareBackup = async (projectID: string) => {
        if (!projectID || backupPlanningID || backingUp) return;
        setBackupPlanningID(projectID);
        setError('');
        setRecoveryBackupResult(null);
        setRecoveryBackupProgress(null);
        try {
            setRecoveryBackupPlan(await GetRecoveryBackupPlan(projectID) as RecoveryBackupPlan);
        } catch (planError) {
            setError(cleanError(planError));
        } finally {
            setBackupPlanningID('');
        }
    };

    const runBackup = async () => {
        if (!backupPlan || backingUp) return;
        setBackingUp(true);
        setRecoveryBackupResult(null);
        setRecoveryBackupProgress({stage: 'connect', message: 'Đang bắt đầu kết nối chỉ đọc…', percent: 2});
        try {
            setRecoveryBackupResult(await RunRecoveryBackup(backupPlan.projectId) as RecoveryBackupResult);
            await onRefresh();
        } catch (backupError) {
            setRecoveryBackupResult({success: false, message: 'Không chạy được sao lưu', detail: cleanError(backupError)});
        } finally {
            setBackingUp(false);
        }
    };

    const closeBackup = () => {
        if (backingUp) return;
        setRecoveryBackupPlan(null);
        setRecoveryBackupProgress(null);
        setRecoveryBackupResult(null);
    };

    return <section className="content-stack recovery-page">
        <section className="configuration-form recovery-manager" aria-busy={loading || Boolean(testingID)}>
            <header className="form-header">
                <div><h2>Website cần khôi phục</h2><p>Sao lưu source, xác minh checksum và quét tĩnh; không sửa hosting.</p></div>
                <div className="domain-header-actions"><span className="detected-badge">{snapshot.projects.length} website</span><button className="button primary" onClick={startNew}><Icon name="recovery" size={16}/>Thêm website</button></div>
            </header>
            {snapshot.storagePath && <div className="recovery-storage"><Icon name="history" size={16}/><span>Đã tự lưu JSON</span><code title={snapshot.storagePath}>{snapshot.storagePath}</code></div>}
            {error && <div className="inline-error recovery-error" role="alert"><Icon name="close" size={17}/><span>{error}</span></div>}
            {snapshot.projects.length === 0 && !loading ? <div className="domain-empty"><span className="empty-icon"><Icon name="recovery" size={24}/></span><div><strong>Chưa có website khôi phục</strong><p>Thêm thông tin FTPS ở biểu mẫu bên dưới.</p></div></div> : <div className="recovery-list" aria-live="polite">
                {snapshot.projects.map(project => {
                    const result = results[project.id];
                    const ready = project.status === 'connection_ready' || project.status === 'backup_ready';
                    const failed = project.status === 'connection_failed' || project.status === 'backup_failed';
                    const running = project.status === 'backup_running';
                    const canBackup = ready || project.status === 'backup_failed';
                    const statusLabel = project.status === 'backup_ready' ? 'Đã sao lưu' : running ? 'Đang sao lưu' : project.status === 'backup_failed' ? 'Sao lưu lỗi' : project.status === 'connection_ready' ? 'Kết nối tốt' : project.status === 'connection_failed' ? 'Kết nối lỗi' : project.passwordStored ? 'Chưa kiểm tra' : 'Thiếu mật khẩu';
                    return <article className="recovery-row" key={project.id}>
                        <div className="recovery-row-main">
                            <span className={ready ? 'domain-status-icon ready' : 'domain-status-icon warning'}><Icon name={ready ? 'check' : 'warning'} size={18}/></span>
                            <div className="recovery-identity"><strong>{project.name}</strong><span>{project.protocol.toUpperCase()} · {project.host}:{project.port}{project.remotePath}</span></div>
                            <span className={ready ? 'project-status ready' : failed ? 'project-status error' : 'project-status running'}>{statusLabel}</span>
                            <div className="recovery-row-actions">
                                <button className="button primary" disabled={!canBackup || running || Boolean(testingID) || Boolean(backupPlanningID) || backingUp} onClick={() => void prepareBackup(project.id)}>{backupPlanningID === project.id ? 'Đang chuẩn bị…' : project.status === 'backup_ready' ? 'Sao lưu lại' : project.status === 'backup_failed' ? 'Thử lại sao lưu' : 'Sao lưu & quét'}</button>
                                <button className="button tertiary" disabled={Boolean(testingID) || backingUp} onClick={() => void testConnection(project.id)}>{testingID === project.id ? 'Đang kiểm tra…' : 'Kiểm tra'}</button>
                                <button className="button secondary" onClick={() => edit(project)}>Cấu hình</button>
                                <button className="button secondary" disabled={openingID === project.id} onClick={() => void openFolder(project.id)}><Icon name="folder" size={16}/>{openingID === project.id ? 'Đang mở…' : 'Dữ liệu'}</button>
                            </div>
                        </div>
                        {project.lastBackupId && <div className="recovery-backup-summary"><Icon name="archive" size={16}/><strong>{formatNumber(project.backupFiles || 0)} file · {formatBytes(project.backupBytes || 0)}</strong><span>{formatNumber(project.findingCount || 0)} cảnh báo · {formatNumber(project.criticalCount || 0)} nghiêm trọng</span><time>{formatDate(project.backupCreatedAt || '')}</time></div>}
                        {result && <div className={result.success ? 'recovery-result success' : 'recovery-result error'} role={result.success ? 'status' : 'alert'}><Icon name={result.success ? 'check' : 'close'} size={16}/><div><strong>{result.message}</strong>{result.detail && <span>{shortText(result.detail, 600)}</span>}</div></div>}
                        {project.lastError && !result && <div className="recovery-last-error"><Icon name="warning" size={15}/><span>{shortText(project.lastError, 260)}</span></div>}
                        <details className="recovery-history"><summary>Lịch sử gần đây · {project.history.length}</summary><div>{project.history.slice(0, 5).map(entry => <p key={entry.id}><span className={entry.status === 'success' ? 'history-dot success' : entry.status === 'error' ? 'history-dot error' : 'history-dot'}/><strong>{entry.message}</strong><time>{formatDate(entry.createdAt)}</time></p>)}</div></details>
                        <div className="recovery-forget">
                            {confirmRemoveID === project.id ? <><span>Chỉ xóa cấu hình và mật khẩu đã lưu; dữ liệu evidence vẫn giữ.</span><button className="button tertiary" disabled={removingID === project.id} onClick={() => setConfirmRemoveID('')}>Huỷ</button><button className="button danger" disabled={removingID === project.id} onClick={() => void remove(project.id)}>{removingID === project.id ? 'Đang xóa…' : 'Xác nhận xóa'}</button></> : <button className="button tertiary recovery-forget-button" onClick={() => setConfirmRemoveID(project.id)}>Xóa khỏi danh sách</button>}
                        </div>
                    </article>;
                })}
            </div>}
        </section>

        <form ref={formRef} className="configuration-form recovery-form" onSubmit={event => void saveAndTest(event)} aria-busy={saving}>
            <header className="form-header"><div><h2>{form.id ? 'Cập nhật kết nối' : 'Thêm website khôi phục'}</h2><p>Ưu tiên FTPS. Mật khẩu lưu trong kho bí mật Windows.</p></div>{form.id && <button type="button" className="button secondary" onClick={startNew}>Tạo hồ sơ mới</button>}</header>
            <div className="form-grid recovery-form-grid">
                <label className="field"><span>Tên website</span><input required maxLength={100} value={form.name} onChange={event => update('name', event.target.value)} placeholder="Website công ty" autoFocus/><small>Tên để nhân viên dễ nhận biết.</small></label>
                <label className="field"><span>Địa chỉ website</span><input type="url" value={form.siteUrl} onChange={event => update('siteUrl', event.target.value)} placeholder="https://example.com" autoComplete="url"/><small>Không bắt buộc; chỉ chấp nhận HTTPS.</small></label>
                <label className="field"><span>Máy chủ FTP</span><input required value={form.host} onChange={event => updateHost(event.target.value)} placeholder="example.com" autoComplete="off"/><small>Nhập domain để tự điền thư mục DirectAdmin.</small></label>
                <div className="recovery-connection-fields">
                    <label className="field"><span>Giao thức</span><select value={form.protocol} onChange={event => { const protocol = event.target.value as 'ftps' | 'ftp'; setForm(current => ({...current, protocol, allowInsecure: protocol === 'ftps' ? false : current.allowInsecure})); setError(''); }}><option value="ftps">FTPS (khuyên dùng)</option><option value="ftp">FTP thường</option></select><small>Mã hóa thông tin đăng nhập.</small></label>
                    <label className="field compact-number"><span>Cổng</span><input required type="number" min={1} max={65535} value={form.port} onChange={event => update('port', Number(event.target.value))}/><small>Mặc định 21.</small></label>
                </div>
                <label className="field"><span>Tài khoản FTP</span><input required maxLength={256} value={form.username} onChange={event => update('username', event.target.value)} autoComplete="username"/><small>Tài khoản có quyền đọc website.</small></label>
                <label className="field"><span>Mật khẩu FTP</span><input type="password" required={!form.id} value={form.password} onChange={event => update('password', event.target.value)} placeholder={form.id ? 'Để trống nếu không đổi' : 'Nhập mật khẩu'} autoComplete="new-password"/><small>{form.id ? 'Để trống để dùng mật khẩu đang lưu.' : 'Không ghi vào recovery.json.'}</small></label>
                <label className="field"><span>Thư mục website</span><input required value={form.remotePath} onChange={event => update('remotePath', event.target.value)} onBlur={() => { if (!form.remotePath) update('remotePath', suggestedRecoveryPath(form.host)); }} placeholder="/domains/example.com/public_html" autoComplete="off"/><small>Tự đề xuất từ máy chủ; vẫn có thể sửa thủ công.</small></label>
                <label className="field"><span>Số luồng tải</span><select value={form.workers} onChange={event => update('workers', Number(event.target.value))}><option value={2}>2 — đường truyền chậm</option><option value={4}>4 — cân bằng</option><option value={6}>6 — đường truyền tốt</option><option value={8}>8 — tối đa</option></select><small>Số kết nối dùng khi tải source.</small></label>
            </div>
            {form.protocol === 'ftp' && <label className="recovery-risk"><input type="checkbox" checked={form.allowInsecure} onChange={event => update('allowInsecure', event.target.checked)}/><span><strong>Tôi xác nhận dùng FTP không mã hóa</strong><small>Tài khoản có thể bị lộ trên mạng. Hãy dùng FTPS nếu hosting hỗ trợ.</small></span></label>}
            <div className="security-note recovery-security"><Icon name="shield" size={17}/><span>Sao lưu chỉ đọc. File từ hosting được đổi thành blob trung tính, không giữ đuôi có thể chạy và không bao giờ được thực thi.</span></div>
            <footer className="form-actions"><span>{form.id ? 'Cấu hình hiện có sẽ được cập nhật.' : 'Tạo hồ sơ mới trong lịch sử JSON.'}</span><div><button type="button" className="button secondary" disabled={saving} onClick={() => setForm(emptyForm())}>Xóa form</button><button className="button primary" type="submit" disabled={saving || Boolean(testingID)}>{saving ? 'Đang lưu…' : 'Lưu và kiểm tra'}</button></div></footer>
        </form>
        {backupPlan && <RecoveryBackupDialog plan={backupPlan} progress={backupProgress} result={backupResult} running={backingUp} onConfirm={runBackup} onOpen={() => openFolder(backupPlan.projectId)} onClose={closeBackup}/>} 
    </section>;
}

function RecoveryBackupDialog({plan, progress, result, running, onConfirm, onOpen, onClose}: {
    plan: RecoveryBackupPlan;
    progress: RecoveryBackupProgress | null;
    result: RecoveryBackupResult | null;
    running: boolean;
    onConfirm: () => Promise<void>;
    onOpen: () => Promise<void>;
    onClose: () => void;
}) {
    const dialogRef = useDialogFocus(onClose, !running);
    return <div className="modal-backdrop" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget && !running) onClose(); }}>
        <section ref={dialogRef} tabIndex={-1} className="install-dialog recovery-backup-dialog" role="dialog" aria-modal="true" aria-labelledby="recovery-backup-title" aria-busy={running}>
            <header className="dialog-header"><div><h2 id="recovery-backup-title">Sao lưu và quét website</h2><p>{plan.name} · {plan.protocol.toUpperCase()}</p></div><button className="icon-button" aria-label="Đóng" disabled={running} onClick={onClose}><Icon name="close" size={18}/></button></header>
            {!progress && !result && <>
                <dl className="plan-details recovery-backup-plan">
                    <div><dt>Máy chủ</dt><dd>{plan.host}</dd></div>
                    <div><dt>Thư mục đọc</dt><dd title={plan.remotePath}>{plan.remotePath}</dd></div>
                    <div><dt>Kết nối tải</dt><dd>{plan.workers}</dd></div>
                    <div><dt>Nơi lưu</dt><dd title={plan.destination}>{plan.destination}</dd></div>
                </dl>
                <div className="security-note"><Icon name="shield" size={17}/><span>OneClick chỉ đọc hosting. Dữ liệu được lưu bằng blob `.ocblob`, xác minh SHA-256 rồi quét tĩnh; không upload, sửa, xóa hoặc chạy source.</span></div>
            </>}
            {progress && !result && <div className="install-progress" role="status" aria-live="polite">
                <div className="progress-line"><span style={{width: `${progress.percent}%`}}/></div>
                <div><strong>{progress.message}</strong><span>{progress.percent}%</span></div>
                <p>{progress.filesTotal ? `${formatNumber(progress.filesDone || 0)}/${formatNumber(progress.filesTotal)} file · ${formatBytes(progress.bytesDone || 0)}/${formatBytes(progress.bytesTotal || 0)}` : 'Đang đọc cấu trúc website trên hosting…'}</p>
            </div>}
            {result && <div className={result.success ? 'install-result success recovery-backup-result' : 'install-result error recovery-backup-result'} role={result.success ? 'status' : 'alert'}>
                <span><Icon name={result.success ? 'check' : 'warning'} size={21}/></span>
                <div><strong>{result.message}</strong>{result.success ? <p>{formatNumber(result.files || 0)} file · {formatBytes(result.bytes || 0)} · {formatNumber(result.findings || 0)} cảnh báo · {formatNumber(result.critical || 0)} nghiêm trọng</p> : <p>Hosting không bị thay đổi; bản sao chưa hoàn chỉnh đã được dọn.</p>}{result.detail && <details className="technical-error"><summary>Chi tiết kỹ thuật</summary><pre>{shortText(result.detail, 1200)}</pre></details>}</div>
            </div>}
            <footer className="dialog-actions">
                {!progress && !result && <><button className="button secondary" onClick={onClose}>Huỷ</button><button className="button primary" onClick={() => void onConfirm()}>Bắt đầu sao lưu</button></>}
                {running && <button className="button primary" disabled>Đang sao lưu…</button>}
                {result && <><button className="button secondary" onClick={() => void onOpen()}><Icon name="folder" size={16}/>Mở dữ liệu</button><button className="button primary" onClick={onClose}>Đóng</button></>}
            </footer>
        </section>
    </div>;
}

function TunnelDialog({domains, subdomain, zone, plan, progress, result, planning, starting, onSubdomainChange, onZoneChange, onPlan, onConfirm, onOpen, onClose}: {
    domains: CloudflareStatus[];
    subdomain: string;
    zone: string;
    plan: TunnelPlan | null;
    progress: TunnelProgress | null;
    result: TunnelResult | null;
    planning: boolean;
    starting: boolean;
    onSubdomainChange: (value: string) => void;
    onZoneChange: (value: string) => void;
    onPlan: () => Promise<void>;
    onConfirm: () => Promise<void>;
    onOpen: (url: string) => Promise<void>;
    onClose: () => void;
}) {
    const dialogRef = useDialogFocus(onClose, !starting && !planning);
    return <div className="modal-backdrop" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}>
        <section ref={dialogRef} tabIndex={-1} className="install-dialog tunnel-dialog" role="dialog" aria-modal="true" aria-labelledby="tunnel-title" aria-busy={starting || planning}>
            <header className="dialog-header"><div><h2 id="tunnel-title">Kết nối domain</h2><p>Tạo HTTPS cố định cho website.</p></div><button className="icon-button" aria-label="Đóng" disabled={starting} onClick={onClose}><Icon name="close" size={18}/></button></header>
            {!progress && !plan && !result && <div className="domain-entry">
                <label className="field"><span>Domain sử dụng</span><select value={zone} onChange={event => onZoneChange(event.target.value)} autoFocus>{domains.map(domain => <option key={domain.zone} value={domain.zone}>{domain.zone}</option>)}</select><small>Chỉ hiển thị domain đã sẵn sàng.</small></label>
                <label className="field"><span>Subdomain</span><div className="hostname-control"><input value={subdomain} onChange={event => onSubdomainChange(event.target.value.toLowerCase())} placeholder="website" aria-label="Tên subdomain"/><span>.{zone}</span></div><small>Địa chỉ đầy đủ: {subdomain ? `${subdomain}.${zone}` : `website.${zone}`}</small></label>
                <div className="security-note"><Icon name="shield" size={17}/><span>OneClick chỉ tạo bản ghi đã xác nhận, không ghi đè subdomain đang có.</span></div>
            </div>}
            {!progress && plan && !result && <>
                <dl className="plan-details environment-plan"><div><dt>Địa chỉ</dt><dd>{plan.address}</dd></div><div><dt>Kết nối</dt><dd>{plan.provider} · {plan.mode}</dd></div><div><dt>Dữ liệu tải</dt><dd>{plan.download}</dd></div><div><dt>Cách ly</dt><dd>{plan.network}</dd></div><div><dt>Truy cập</dt><dd>{plan.exposure}</dd></div></dl>
                <div className="security-note"><Icon name="shield" size={17}/><span>Website sẽ công khai trên Internet. Không mở port và không lộ IP máy tính.</span></div>
            </>}
            {progress && !result && <div className="install-progress" aria-live="polite"><div className="progress-line"><span style={{width: `${progress.percent}%`}}/></div><div><strong>{progress.message}</strong><span>{progress.percent}%</span></div><p>Có thể tiếp tục làm việc; domain sẽ chỉ được lưu sau khi HTTPS kiểm tra thành công.</p></div>}
            {result && <div className={result.success ? 'install-result success' : 'install-result error'} role={result.success ? 'status' : 'alert'}><span><Icon name={result.success ? 'check' : 'close'} size={22}/></span><div><strong>{result.message}</strong>{result.success && result.url ? <p className="verified-url">{result.url}</p> : <p>Website chưa được public; runtime và source vẫn được giữ nguyên.</p>}{result.detail && <details className="technical-error"><summary>Chi tiết kỹ thuật</summary><pre>{shortText(result.detail, 1200)}</pre></details>}</div></div>}
            <footer className="dialog-actions">
                {!progress && !plan && !result && <button className="button secondary" disabled={planning} onClick={onClose}>Huỷ</button>}
                {!progress && !plan && !result && <button className="button primary" disabled={planning || !subdomain.trim() || !zone} onClick={() => void onPlan()}>{planning ? 'Đang kiểm tra…' : 'Kiểm tra địa chỉ'}</button>}
                {!progress && plan && !result && <button className="button secondary" disabled={starting} onClick={() => onSubdomainChange(subdomain)}>Đổi địa chỉ</button>}
                {!progress && plan && !result && <button className="button primary" disabled={starting} autoFocus onClick={() => void onConfirm()}>{starting ? 'Đang kết nối…' : 'Xuất bản website'}</button>}
                {result && !result.success && <button className="button secondary" onClick={onClose}>Đóng</button>}
                {result && !result.success && <button className="button primary" onClick={() => void (plan ? onConfirm() : onPlan())}>Thử lại</button>}
                {result?.success && result.url && <button className="button secondary" onClick={() => void onOpen(result.url || '')}>Mở website</button>}
                {result?.success && <button className="button primary" onClick={onClose}>Hoàn tất</button>}
            </footer>
        </section>
    </div>;
}

function CopyDialog({plan, progress, result, copying, onConfirm, onClose}: {
    plan: CopyPlan;
    progress: CopyProgress | null;
    result: CopyResult | null;
    copying: boolean;
    onConfirm: () => Promise<void>;
    onClose: () => void;
}) {
    const dialogRef = useDialogFocus(onClose, !copying);
    return <div className="modal-backdrop" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}>
        <section ref={dialogRef} tabIndex={-1} className="install-dialog copy-dialog" role="dialog" aria-modal="true" aria-labelledby="copy-title" aria-busy={copying}>
            <header className="dialog-header">
                <div><h2 id="copy-title">Sao chép website</h2><p>Tạo bản sao riêng trong máy ảo.</p></div>
                <button className="icon-button" aria-label="Đóng" disabled={copying} onClick={onClose}><Icon name="close" size={18}/></button>
            </header>
            {!progress && !result && <>
                <dl className="plan-details copy-plan">
                    <div><dt>Số file</dt><dd>{formatNumber(plan.fileCount)} file</dd></div>
                    <div><dt>Dung lượng</dt><dd>{formatBytes(plan.totalBytes)}</dd></div>
                    <div><dt>Đã loại trừ</dt><dd>{formatNumber(plan.excludedCount)} mục</dd></div>
                    <div><dt>File bí mật</dt><dd>{plan.secretExcluded > 0 ? `${formatNumber(plan.secretExcluded)} file không sao chép` : 'Không phát hiện'}</dd></div>
                </dl>
                <div className="security-note"><Icon name="shield" size={17}/><span>Không sao chép .env, key, certificate, log, cache hoặc database dump. Source trên máy không bị chỉnh sửa.</span></div>
            </>}
            {progress && !result && <div className="install-progress" aria-live="polite">
                <div className="progress-line"><span style={{width: `${progress.percent}%`}}/></div>
                <div><strong>{progress.message}</strong><span>{progress.percent}%</span></div>
                <p>Website chỉ được đóng gói và chuyển vào máy ảo; chưa chạy hoặc public.</p>
            </div>}
            {result && <div className={result.success ? 'install-result success' : 'install-result error'} role={result.success ? 'status' : 'alert'}>
                <span><Icon name={result.success ? 'check' : 'close'} size={22}/></span>
                <div><strong>{result.message}</strong>{result.success && <p>{formatNumber(result.fileCount || 0)} file · {formatBytes(result.totalBytes || 0)}</p>}{!result.success && <p>Source trên máy không bị thay đổi; chưa có website nào được chạy hoặc public.</p>}{result.detail && <details className="technical-error"><summary>Chi tiết kỹ thuật</summary><pre>{shortText(result.detail, 1200)}</pre></details>}</div>
            </div>}
            <footer className="dialog-actions">
                {!result && <button className="button secondary" disabled={copying} onClick={onClose}>Huỷ</button>}
                {!result && <button className="button primary" disabled={copying} autoFocus onClick={() => void onConfirm()}>{copying ? 'Đang sao chép…' : 'Bắt đầu sao chép'}</button>}
                {result && !result.success && <button className="button secondary" onClick={onClose}>Đóng</button>}
                {result && !result.success && <button className="button primary" onClick={() => void onConfirm()}>Thử lại</button>}
                {result?.success && <button className="button primary" onClick={onClose}>Hoàn tất</button>}
            </footer>
        </section>
    </div>;
}

function RuntimeDialog({plan, progress, result, installing, onConfirm, onClose}: {
    plan: RuntimePlan;
    progress: RuntimeProgress | null;
    result: RuntimeResult | null;
    installing: boolean;
    onConfirm: () => Promise<void>;
    onClose: () => void;
}) {
    const dialogRef = useDialogFocus(onClose, !installing);
    return <div className="modal-backdrop" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}>
        <section ref={dialogRef} tabIndex={-1} className="install-dialog runtime-dialog" role="dialog" aria-modal="true" aria-labelledby="runtime-title" aria-busy={installing}>
            <header className="dialog-header">
                <div><h2 id="runtime-title">Cài môi trường chạy</h2><p>Chuẩn bị website trực tiếp trên Ubuntu.</p></div>
                <button className="icon-button" aria-label="Đóng" disabled={installing} onClick={onClose}><Icon name="close" size={18}/></button>
            </header>
            {!progress && !result && <>
                <dl className="plan-details environment-plan">
                    <div><dt>Bộ chạy</dt><dd>{plan.runtime}</dd></div>
                    <div><dt>Database</dt><dd>{plan.database}</dd></div>
                    <div><dt>Vùng riêng</dt><dd>{plan.services} thành phần · {plan.resources}</dd></div>
                    <div><dt>Dữ liệu tải</dt><dd>{plan.download}</dd></div>
                    <div><dt>Mạng</dt><dd>{plan.network}</dd></div>
                </dl>
                <div className="security-note"><Icon name="shield" size={17}/><span>User Linux, PHP sandbox và database được tách riêng. Không Docker, không mở cổng và chưa public.</span></div>
            </>}
            {progress && !result && <div className="install-progress" aria-live="polite">
                <div className="progress-line"><span style={{width: `${progress.percent}%`}}/></div>
                <div><strong>{progress.message}</strong><span>{progress.percent}%</span></div>
                <p>Lần đầu Ubuntu sẽ cài các gói dùng chung. Dự án sau không phải cài lại.</p>
            </div>}
            {result && <div className={result.success ? 'install-result success' : 'install-result error'} role={result.success ? 'status' : 'alert'}>
                <span><Icon name={result.success ? 'check' : 'close'} size={22}/></span>
                <div><strong>{result.message}</strong>{result.success ? <p>{result.services || 0} thành phần riêng · đã kiểm tra cách ly · chưa public</p> : <p>Snapshot gốc không bị thay đổi; chưa mở truy cập Internet.</p>}{result.detail && <details className="technical-error"><summary>Chi tiết kỹ thuật</summary><pre>{shortText(result.detail, 1200)}</pre></details>}</div>
            </div>}
            <footer className="dialog-actions">
                {!result && <button className="button secondary" disabled={installing} onClick={onClose}>Huỷ</button>}
                {!result && <button className="button primary" disabled={installing} autoFocus onClick={() => void onConfirm()}>{installing ? 'Đang cài…' : (plan.existing ? 'Kiểm tra và cài lại' : 'Cài môi trường')}</button>}
                {result && !result.success && <button className="button secondary" onClick={onClose}>Đóng</button>}
                {result && !result.success && <button className="button primary" onClick={() => void onConfirm()}>Thử lại</button>}
                {result?.success && <button className="button primary" onClick={onClose}>Hoàn tất</button>}
            </footer>
        </section>
    </div>;
}

function DatabaseDialog({plan, mode, progress, result, planning, importing, onUseAutomatic, onChooseFile, onConfirm, onClose}: {
    plan: DatabasePlan;
    mode: DatabaseMode;
    progress: DatabaseProgress | null;
    result: DatabaseResult | null;
    planning: boolean;
    importing: boolean;
    onUseAutomatic: () => void;
    onChooseFile: () => Promise<void>;
    onConfirm: () => Promise<void>;
    onClose: () => void;
}) {
    const dialogRef = useDialogFocus(onClose, !importing);
    return <div className="modal-backdrop" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}>
        <section ref={dialogRef} tabIndex={-1} className="install-dialog database-dialog" role="dialog" aria-modal="true" aria-labelledby="database-title" aria-busy={importing || planning}>
            <header className="dialog-header">
                <div><h2 id="database-title">Sao chép database</h2><p>Đưa dữ liệu WordPress vào đúng máy ảo của website.</p></div>
                <button className="icon-button" aria-label="Đóng" disabled={importing} onClick={onClose}><Icon name="close" size={18}/></button>
            </header>
            {!progress && !result && <div className="database-plan">
                <div className="database-source-list" role="radiogroup" aria-label="Nguồn database">
                    <div className={mode === 'automatic' ? 'database-source active' : 'database-source'}>
                        <label><input type="radio" name="database-source" checked={mode === 'automatic'} disabled={planning} onChange={onUseAutomatic}/><span><strong>Tự lấy từ WordPress</strong><small>{mode === 'automatic' ? (plan.automaticMessage || 'Đọc cấu hình WordPress trên máy.') : 'Đọc wp-config.php và xuất database cục bộ.'}</small></span></label>
                    </div>
                    <div className={mode === 'file' ? 'database-source active' : 'database-source'}>
                        <label><input type="radio" name="database-source" checked={mode === 'file'} disabled={planning} onChange={() => void onChooseFile()}/><span><strong>Chọn file database</strong><small>{mode === 'file' && plan.importPath ? plan.importPath : 'Hỗ trợ .sql và .sql.gz.'}</small></span></label>
                        <button type="button" className="button secondary" disabled={planning} onClick={() => void onChooseFile()}>Chọn file</button>
                    </div>
                </div>
                {plan.ready ? <dl className="plan-details database-details">
                    <div><dt>Nguồn</dt><dd>{plan.source}</dd></div>
                    {plan.database && <div><dt>Database</dt><dd>{plan.database}</dd></div>}
                    {plan.tool && <div><dt>Công cụ</dt><dd>{plan.tool}</dd></div>}
                    {plan.importBytes ? <div><dt>Dung lượng</dt><dd>{formatBytes(plan.importBytes)}</dd></div> : null}
                    <div><dt>Tiền tố bảng</dt><dd>{plan.tablePrefix || 'Kiểm tra khi nhập'}</dd></div>
                </dl> : <div className="inline-warning" role="status"><Icon name="warning" size={17}/><span>{plan.automaticMessage || 'Chọn file database để tiếp tục.'}</span></div>}
                <div className="security-note"><Icon name="shield" size={17}/><span>{plan.replacementNotice}</span></div>
            </div>}
            {progress && !result && <div className="install-progress" aria-live="polite">
                <div className="progress-line"><span style={{width: `${progress.percent}%`}}/></div>
                <div><strong>{progress.message}</strong><span>{progress.percent}%</span></div>
                <p>Database trên máy chỉ được đọc. File tạm sẽ được xoá sau khi kiểm tra xong.</p>
            </div>}
            {result && <div className={result.success ? 'install-result success' : 'install-result error'} role={result.success ? 'status' : 'alert'}>
                <span><Icon name={result.success ? 'check' : 'close'} size={22}/></span>
                <div><strong>{result.message}</strong>{result.success ? <p>{formatNumber(result.tables || 0)} bảng · {formatBytes(result.bytes || 0)} · tiền tố {result.tablePrefix}</p> : <p>Runtime và source vẫn được giữ nguyên; website chưa public.</p>}{result.detail && <details className="technical-error"><summary>Chi tiết kỹ thuật</summary><pre>{shortText(result.detail, 1200)}</pre></details>}</div>
            </div>}
            <footer className="dialog-actions">
                {!result && <button className="button secondary" disabled={importing} onClick={onClose}>Huỷ</button>}
                {!result && <button className="button primary" disabled={importing || planning || !plan.ready} autoFocus onClick={() => void onConfirm()}>{importing ? 'Đang sao chép…' : 'Sao chép database'}</button>}
                {result && !result.success && <button className="button secondary" onClick={onClose}>Đóng</button>}
                {result && !result.success && <button className="button primary" onClick={() => void onConfirm()}>Thử lại</button>}
                {result?.success && <button className="button primary" onClick={onClose}>Hoàn tất</button>}
            </footer>
        </section>
    </div>;
}

function InstallDialog({plan, progress, result, installing, onConfirm, onClose}: {
    plan: DependencyPlan;
    progress: InstallerProgress | null;
    result: InstallerResult | null;
    installing: boolean;
    onConfirm: () => Promise<void>;
    onClose: () => void;
}) {
    const dialogRef = useDialogFocus(onClose, !installing);
    return (
        <div className="modal-backdrop" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}>
            <section ref={dialogRef} tabIndex={-1} className="install-dialog" role="dialog" aria-modal="true" aria-labelledby="install-title" aria-busy={installing}>
                <header className="dialog-header">
                    <div><h2 id="install-title">{plan.title}</h2><p>{plan.description}</p></div>
                    <button className="icon-button" aria-label="Đóng" disabled={installing} onClick={onClose}><Icon name="close" size={18}/></button>
                </header>
                {!progress && !result && <dl className="plan-details">
                    <div><dt>Nhà phát hành</dt><dd>{plan.publisher}</dd></div>
                    <div><dt>Phiên bản</dt><dd>{plan.version}</dd></div>
                    <div><dt>Nguồn cài đặt</dt><dd>{plan.source}</dd></div>
                    <div><dt>Dung lượng tải</dt><dd>{plan.downloadSize}</dd></div>
                    <div><dt>Quyền quản trị</dt><dd>{plan.requiresAdmin ? 'Có — Windows sẽ hỏi xác nhận' : 'Không'}</dd></div>
                </dl>}
                {progress && !result && <div className="install-progress" aria-live="polite">
                    <div className="progress-line"><span style={{width: `${progress.percent}%`}}/></div>
                    <div><strong>{progress.message}</strong><span>{progress.percent}%</span></div>
                    {progress.stage === 'permission' && <p>Chọn “Yes”, sau đó chờ khoảng 2–5 phút. Ứng dụng vẫn đang hoạt động.</p>}
                    {progress.stage === 'install' && <p>Không cần thao tác thêm. Trạng thái sẽ tự cập nhật khi hoàn tất.</p>}
                </div>}
                {result && <div className={result.success ? 'install-result success' : 'install-result error'} role={result.success ? 'status' : 'alert'}>
                    <span><Icon name={result.success ? 'check' : 'close'} size={22}/></span><div><strong>{result.message}</strong>{result.detail && <p>{shortText(result.detail, 600)}</p>}</div>
                </div>}
                <footer className="dialog-actions">
                    {!result && <button className="button secondary" disabled={installing} onClick={onClose}>Huỷ</button>}
                    {!result && <button className="button primary" disabled={installing} autoFocus onClick={() => void onConfirm()}>{installing ? 'Đang cài đặt…' : 'Cài đặt ngay'}</button>}
                    {result && !result.success && <button className="button secondary" onClick={onClose}>Đóng</button>}
                    {result && !result.success && <button className="button primary" onClick={() => void onConfirm()}>Thử lại</button>}
                    {result?.success && <button className="button primary" onClick={onClose}>Hoàn tất</button>}
                </footer>
            </section>
        </div>
    );
}

function SystemView({report, loading, progress, onRefresh, onContinue, onAction}: {
    report: ReadinessReport | null;
    loading: boolean;
    progress: {ready: number; total: number; percent: number};
    onRefresh: () => Promise<void>;
    onContinue: () => Promise<void>;
    onAction: (check: ReadinessCheck) => Promise<void>;
}) {
    return <section className="content-stack">
        <div className="system-summary">
            <div className="summary-copy"><span className={report?.ready ? 'summary-indicator ready' : 'summary-indicator pending'}><Icon name={report?.ready ? 'check' : 'warning'} size={17}/></span><div><h2>Tình trạng hệ thống</h2><p>{loading ? 'Đang kiểm tra…' : `${progress.ready}/${progress.total} thành phần sẵn sàng`}</p></div></div>
            <div className="summary-progress" aria-label={`${progress.ready} trên ${progress.total} mục đã sẵn sàng`}><span style={{width: `${loading ? 8 : progress.percent}%`}}/></div>
            <button className="button secondary" onClick={() => void onRefresh()} disabled={loading}><Icon name="refresh" size={17}/>{loading ? 'Đang kiểm tra…' : 'Kiểm tra lại'}</button>
        </div>

        <div className="system-table-wrap" aria-live="polite">
            <table className="system-table">
                <thead><tr><th>Thành phần</th><th>Trạng thái</th><th>Thông tin</th><th aria-label="Hành động"/></tr></thead>
                <tbody>{loading && !report
                    ? Array.from({length: 6}).map((_, index) => <tr className="table-skeleton" key={index}><td colSpan={4}><span/></td></tr>)
                    : report?.checks.map(check => <tr className={`status-${check.status}`} key={check.id}>
                        <td><div className="component-name"><span className="check-status-icon"><Icon name={statusCopy[check.status].icon} size={15}/></span><strong>{check.title}</strong></div></td>
                        <td><span className="status-label">{statusCopy[check.status].label}</span></td>
                        <td className="check-copy"><p>{check.summary}</p>{check.detail && <details><summary>Chi tiết</summary><div>{check.detail}</div></details>}</td>
                        <td className="action-cell">{check.actionLabel && <button className="button tertiary" onClick={() => void onAction(check)}>{check.actionLabel}</button>}</td>
                    </tr>)}</tbody>
            </table>
        </div>

        <div className="action-bar"><div><strong>{report?.ready ? 'Máy đã sẵn sàng' : 'Chưa thể xuất bản'}</strong><span>{report?.ready ? 'Bạn có thể chọn website.' : 'Hoàn tất các mục còn thiếu.'}</span></div><button className="button primary" onClick={() => void onContinue()} disabled={!report?.ready || loading}><Icon name="folder" size={18}/>Chọn website<Icon name="arrow" size={17}/></button></div>
    </section>;
}

function Overview({report, projects, loading, onOpenProject, onSetup, onChooseProject}: {
    report: ReadinessReport | null;
    projects: StoredProject[];
    loading: boolean;
    onOpenProject: (project: StoredProject) => void;
    onSetup: () => void;
    onChooseProject: () => Promise<void>;
}) {
    return <section className="content-stack">
        <div className={report?.ready ? 'readiness-card success' : 'readiness-card warning'}><span className="large-status-icon"><Icon name={report?.ready ? 'check' : 'warning'} size={25}/></span><div><h2>{report?.ready ? 'Máy đã sẵn sàng' : 'Máy cần được thiết lập'}</h2><p>{report?.ready ? 'Có thể chuẩn bị website mới.' : 'Hoàn tất kiểm tra trước khi xuất bản.'}</p></div><button className="button secondary" onClick={onSetup}>{report?.ready ? 'Xem kiểm tra' : 'Thiết lập ngay'}</button></div>
        <div className="section-heading"><div><h2>Website gần đây</h2><p>{projects.length} dự án đã lưu</p></div>{projects.length > 0 && <button className="button secondary" onClick={() => void onChooseProject()} disabled={!report?.ready}><Icon name="folder" size={17}/>Thêm website</button>}</div>
        {loading ? <div className="history-loading" role="status">Đang đọc lịch sử…</div> : projects.length > 0 ? <div className="recent-projects">
            {projects.slice(0, 4).map(project => <ProjectRow key={project.id} project={project} onOpen={() => onOpenProject(project)}/>) }
        </div> : <div className="empty-state"><span className="empty-icon"><Icon name="globe" size={29}/></span><h2>Chưa có website nào</h2><p>Chọn thư mục Laragon, XAMPP hoặc dự án PHP.</p><button className="button primary" disabled={!report?.ready} onClick={() => void onChooseProject()}><Icon name="folder" size={18}/>Chọn thư mục website</button></div>}
    </section>;
}

function Websites({report, projects, projectsLoading, discoveredProjects, discoveryLoading, storagePath, selectedProject, projectInfo, settings, configured, environment, environmentPlanning, copyPlanning, runtimePlanning, databasePlanning, tunnelPlanning, stoppingTunnel, projectData, projectDataLoading, backupProgress, backupResult, backingUp, cloudflareDomains, loading, screen, onOpenProject, onOpenDiscovered, onRefreshDiscovery, onScreenChange, onSettingsChange, onConfirmSettings, onCreateEnvironment, onCopyWebsite, onInstallRuntime, onImportDatabase, onStartTunnel, onStopTunnel, onOpenURL, onOpenSource, onOpenSourceFolder, onCreateBackup, onOpenBackup, onCloudflareSetup, onSetup, onChooseProject}: {
    report: ReadinessReport | null;
    projects: StoredProject[];
    projectsLoading: boolean;
    discoveredProjects: ProjectInfo[];
    discoveryLoading: boolean;
    storagePath: string;
    selectedProject: string;
    projectInfo: ProjectInfo | null;
    settings: ProjectSettings | null;
    configured: boolean;
    environment: EnvironmentResult | null;
    environmentPlanning: boolean;
    copyPlanning: boolean;
    runtimePlanning: boolean;
    databasePlanning: boolean;
    tunnelPlanning: boolean;
    stoppingTunnel: boolean;
    projectData: ProjectDataStatus | null;
    projectDataLoading: boolean;
    backupProgress: BackupProgress | null;
    backupResult: BackupResult | null;
    backingUp: boolean;
    cloudflareDomains: CloudflareStatus[];
    loading: boolean;
    screen: WebsiteScreen;
    onOpenProject: (project: StoredProject) => void;
    onOpenDiscovered: (project: ProjectInfo) => void;
    onRefreshDiscovery: () => Promise<void>;
    onScreenChange: (screen: WebsiteScreen) => void;
    onSettingsChange: (settings: ProjectSettings) => void;
    onConfirmSettings: (settings: ProjectSettings) => Promise<void>;
    onCreateEnvironment: () => Promise<void>;
    onCopyWebsite: () => Promise<void>;
    onInstallRuntime: () => Promise<void>;
    onImportDatabase: () => Promise<void>;
    onStartTunnel: () => void;
    onStopTunnel: () => Promise<void>;
    onOpenURL: (url: string) => Promise<void>;
    onOpenSource: () => void;
    onOpenSourceFolder: () => Promise<void>;
    onCreateBackup: () => Promise<void>;
    onOpenBackup: () => Promise<void>;
    onCloudflareSetup: () => void;
    onSetup: () => void;
    onChooseProject: () => Promise<void>;
}) {
    const activeRecord = projects.find(item => samePath(item.path, selectedProject));

    if (screen === 'configure' && projectInfo && settings) {
        return <ProjectConfiguration info={projectInfo} settings={settings} onChange={onSettingsChange} onBack={() => onScreenChange(configured ? 'detail' : 'list')} onConfirm={onConfirmSettings}/>;
    }

    if (screen === 'source' && activeRecord) {
        return <SourceManager key={activeRecord.id} project={activeRecord} onBack={() => onScreenChange('detail')} onOpenFolder={onOpenSourceFolder}/>;
    }

    const readyDomains = cloudflareDomains.filter(domain => domain.connected && domain.tokenStored && domain.dnsReady);
    if (screen === 'detail' && activeRecord) {
        return <ProjectDetail project={activeRecord} report={report} environment={environment} readyDomainCount={readyDomains.length} environmentPlanning={environmentPlanning} copyPlanning={copyPlanning} runtimePlanning={runtimePlanning} databasePlanning={databasePlanning} tunnelPlanning={tunnelPlanning} stoppingTunnel={stoppingTunnel} projectData={projectData} projectDataLoading={projectDataLoading} backupProgress={backupProgress} backupResult={backupResult} backingUp={backingUp} onBack={() => onScreenChange('list')} onEdit={() => onScreenChange('configure')} onCreateEnvironment={onCreateEnvironment} onCopyWebsite={onCopyWebsite} onInstallRuntime={onInstallRuntime} onImportDatabase={onImportDatabase} onStartTunnel={onStartTunnel} onStopTunnel={onStopTunnel} onOpenURL={onOpenURL} onOpenSource={onOpenSource} onCreateBackup={onCreateBackup} onOpenBackup={onOpenBackup} onCloudflareSetup={onCloudflareSetup} onSetup={onSetup}/>;
    }

    const readyCount = projects.filter(item => item.stage === 'database_ready' || item.stage === 'tunnel_failed' || item.stage === 'public').length;
    const errorCount = projects.filter(item => item.status === 'error').length;
    const totalCount = projects.length + discoveredProjects.length;

    return <section className="content-stack">
        {!report?.ready && <div className="setup-warning" role="status"><span><Icon name="warning" size={17}/></span><div><strong>Máy còn thiếu thành phần</strong><p>Vẫn có thể xem lịch sử; cần hoàn tất kiểm tra máy trước khi tiếp tục.</p></div><button className="button secondary" onClick={onSetup}>Kiểm tra máy</button></div>}
        <div className="project-summary" aria-label="Tóm tắt dự án">
            <div><strong>{totalCount}</strong><span>Tổng website</span></div>
            <div><strong>{readyCount}</strong><span>Môi trường sẵn sàng</span></div>
            <div><strong>{errorCount + discoveredProjects.length}</strong><span>Cần thiết lập</span></div>
            <div className="storage-state"><Icon name="history" size={17}/><span>{storagePath ? 'Đã tự động lưu JSON' : 'Chưa có file lịch sử'}</span></div>
        </div>
        <div className="section-heading"><div><h2>Danh sách website</h2><p>Chọn một dự án để xem và tiếp tục.</p></div><button className="button primary" disabled={loading || !report?.ready} onClick={() => void onChooseProject()}><Icon name="folder" size={18}/>{loading ? 'Đang đọc…' : 'Thêm website'}</button></div>
        {projectsLoading ? <div className="history-loading" role="status">Đang đọc lịch sử…</div> : <div className="project-list">
            {projects.map(project => <ProjectRow key={project.id} project={project} onOpen={() => onOpenProject(project)}/>) }
            {projectInfo && !configured && <article className="project-card active-project"><span className="project-icon"><Icon name="globe"/></span><div><h3>{projectInfo.name}</h3><p>{projectInfo.path}</p><small>{projectInfo.kindLabel} · Chưa lưu</small></div><span className="draft-badge">Chờ cấu hình</span><button className="button primary" onClick={() => onScreenChange('configure')}>Cấu hình</button></article>}
        </div>}

        {(discoveryLoading || discoveredProjects.length > 0) && <section className="discovery-block" aria-labelledby="discovery-title">
            <div className="section-heading compact-heading"><div><h2 id="discovery-title">Đã tìm thấy trên máy</h2><p>Website nằm trong Laragon, XAMPP hoặc WampServer.</p></div><button className="button tertiary" disabled={discoveryLoading} onClick={() => void onRefreshDiscovery()}><Icon name="refresh" size={16}/>{discoveryLoading ? 'Đang quét…' : 'Quét lại'}</button></div>
            {discoveryLoading ? <div className="history-loading" role="status">Đang nhận diện website…</div> : <div className="project-list discovered-list">{discoveredProjects.map(info => <DetectedProjectRow key={info.path} info={info} disabled={!report?.ready} onOpen={() => onOpenDiscovered(info)}/>)}</div>}
        </section>}

        {!projectsLoading && !discoveryLoading && projects.length === 0 && discoveredProjects.length === 0 && !projectInfo && <div className="empty-state compact"><span className="empty-icon"><Icon name="folder" size={28}/></span><h2>Chưa tìm thấy website</h2><p>Đặt dự án trong laragon\www hoặc xampp\htdocs.</p><button className="button primary" disabled={loading || !report?.ready} onClick={() => void onChooseProject()}>{loading ? 'Đang đọc thư mục…' : 'Chọn thư mục khác'}</button></div>}
    </section>;
}

function SourceManager({project, onBack, onOpenFolder}: {project: StoredProject; onBack: () => void; onOpenFolder: () => Promise<void>}) {
    const [listing, setListing] = useState<SourceListing | null>(null);
    const [file, setFile] = useState<SourceFile | null>(null);
    const [content, setContent] = useState('');
    const [loadingList, setLoadingList] = useState(true);
    const [loadingFile, setLoadingFile] = useState(false);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');
    const [result, setResult] = useState<SourceSaveResult | null>(null);
    const [leavePending, setLeavePending] = useState(false);
    const listRequest = useRef(0);
    const fileRequest = useRef(0);
    const dirty = file !== null && content !== file.content;

    const loadDirectory = useCallback(async (directory: string) => {
        const request = ++listRequest.current;
        setLoadingList(true);
        setError('');
        try {
            const next = await ListProjectSource(project.path, directory) as SourceListing;
            if (request === listRequest.current) setListing({...next, entries: next.entries || []});
        } catch (loadError) {
            if (request === listRequest.current) setError(`Không đọc được thư mục: ${cleanError(loadError)}`);
        } finally {
            if (request === listRequest.current) setLoadingList(false);
        }
    }, [project.path]);

    useEffect(() => { void loadDirectory(''); }, [loadDirectory]);
    useEffect(() => {
        const handleBeforeUnload = (event: BeforeUnloadEvent) => {
            if (!dirty) return;
            event.preventDefault();
            event.returnValue = '';
        };
        window.addEventListener('beforeunload', handleBeforeUnload);
        return () => window.removeEventListener('beforeunload', handleBeforeUnload);
    }, [dirty]);

    const guardUnsaved = () => {
        if (!dirty) return true;
        setError('File đang có thay đổi chưa lưu. Hãy lưu hoặc hoàn tác trước khi mở mục khác.');
        return false;
    };

    const openDirectory = (directory: string) => {
        if (!guardUnsaved()) return;
        setFile(null);
        setContent('');
        setResult(null);
        void loadDirectory(directory);
    };

    const openFile = async (entry: SourceEntry) => {
        if (!entry.editable) {
            setError(entry.blockedReason || 'Mục này được bảo vệ.');
            return;
        }
        if (entry.kind === 'directory') {
            openDirectory(entry.path);
            return;
        }
        if (!guardUnsaved()) return;
        const request = ++fileRequest.current;
        setLoadingFile(true);
        setError('');
        setResult(null);
        try {
            const opened = await ReadProjectSourceFile(project.path, entry.path) as SourceFile;
            if (request === fileRequest.current) {
                setFile(opened);
                setContent(opened.content);
                setLeavePending(false);
            }
        } catch (openError) {
            if (request === fileRequest.current) setError(`Không mở được file: ${cleanError(openError)}`);
        } finally {
            if (request === fileRequest.current) setLoadingFile(false);
        }
    };

    const saveFile = async () => {
        if (!file || !dirty || saving) return;
        setSaving(true);
        setError('');
        setResult(null);
        try {
            const saved = await SaveProjectSourceFile({projectPath: project.path, path: file.path, content, expectedSha: file.sha256}) as SourceSaveResult;
            setResult(saved);
            if (saved.success) {
                setFile({...file, content, sha256: saved.sha256 || file.sha256, bytes: saved.bytes ?? new TextEncoder().encode(content).length, modifiedAt: saved.savedAt || new Date().toISOString()});
                setLeavePending(false);
                void loadDirectory(listing?.directory || '');
            }
        } catch (saveError) {
            setResult({success: false, message: 'Không lưu được source', detail: cleanError(saveError)});
        } finally {
            setSaving(false);
        }
    };

    const reloadFile = async () => {
        if (!file) return;
        const request = ++fileRequest.current;
        setLoadingFile(true);
        setError('');
        setResult(null);
        try {
            const opened = await ReadProjectSourceFile(project.path, file.path) as SourceFile;
            if (request === fileRequest.current) {
                setFile(opened);
                setContent(opened.content);
                setLeavePending(false);
            }
        } catch (reloadError) {
            if (request === fileRequest.current) setError(`Không mở lại được file: ${cleanError(reloadError)}`);
        } finally {
            if (request === fileRequest.current) setLoadingFile(false);
        }
    };

    const breadcrumbs = (listing?.directory || '').split('/').filter(Boolean);
    return <section className="content-stack source-manager-page" aria-labelledby="source-manager-title">
        <div className="detail-toolbar source-toolbar">
            <button className="button secondary" onClick={() => dirty ? setLeavePending(true) : onBack()}><Icon name="back" size={17}/>Chi tiết website</button>
            <button className="button tertiary" onClick={() => void onOpenFolder()}><Icon name="folder" size={17}/>Mở bằng Explorer</button>
        </div>

        <article className="source-manager-heading">
            <span className="project-detail-icon"><Icon name="file" size={21}/></span>
            <div><h2 id="source-manager-title">{project.name}</h2><p title={project.path}>{project.path}</p></div>
            <span className="source-scope-badge">Source gốc</span>
        </article>

        <div className="source-safety-note" role="note"><Icon name="shield" size={17}/><span>Chỉ sửa source gốc trên máy. Website public chưa thay đổi cho tới khi có bước cập nhật riêng.</span></div>
        {leavePending && <div className="source-leave-warning" role="alert"><Icon name="warning" size={18}/><div><strong>Còn thay đổi chưa lưu</strong><p>Rời màn hình sẽ bỏ phần đang sửa.</p></div><button className="button tertiary" onClick={() => setLeavePending(false)}>Tiếp tục sửa</button><button className="button danger" onClick={onBack}>Bỏ thay đổi</button></div>}
        {error && <div className="source-inline-message error" role="alert"><Icon name="warning" size={17}/><span>{error}</span><button aria-label="Đóng lỗi" onClick={() => setError('')}><Icon name="close" size={16}/></button></div>}

        <div className="source-workspace">
            <aside className="source-browser" aria-label="Cây thư mục source">
                <header><strong>Thư mục</strong><button className="icon-button" aria-label="Tải lại thư mục" disabled={loadingList || dirty} onClick={() => void loadDirectory(listing?.directory || '')}><Icon name="refresh" size={16}/></button></header>
                <nav className="source-breadcrumbs" aria-label="Đường dẫn source">
                    <button disabled={loadingList} onClick={() => openDirectory('')}>{project.name}</button>
                    {breadcrumbs.map((part, index) => <span key={`${part}-${index}`}><b>/</b><button disabled={loadingList} onClick={() => openDirectory(breadcrumbs.slice(0, index + 1).join('/'))}>{part}</button></span>)}
                </nav>
                <div className="source-entry-list" aria-busy={loadingList}>
                    {loadingList ? <div className="source-browser-state" role="status">Đang đọc thư mục…</div> : <>
                        {listing?.parent !== undefined && listing.directory && <button className="source-entry directory" onClick={() => openDirectory(listing.parent || '')}><Icon name="back" size={16}/><span><strong>Thư mục trên</strong><small>{listing.parent || project.name}</small></span></button>}
                        {(listing?.entries || []).map(entry => <button key={entry.path} className={`source-entry ${entry.kind} ${file?.path === entry.path ? 'active' : ''} ${entry.editable ? '' : 'blocked'}`} aria-disabled={!entry.editable} title={entry.blockedReason || entry.path} onClick={() => void openFile(entry)}><Icon name={entry.editable ? entry.kind === 'directory' ? 'folder' : 'file' : 'lock'} size={17}/><span><strong>{entry.name}</strong><small>{entry.kind === 'file' ? formatBytes(entry.size) : entry.blockedReason || 'Thư mục'}</small></span></button>)}
                        {!listing?.entries?.length && <div className="source-browser-state">Thư mục trống</div>}
                    </>}
                </div>
            </aside>

            <section className="source-editor" aria-label="Trình chỉnh sửa source">
                <header><div>{file ? <><strong>{file.name}</strong><code title={file.path}>{file.path}</code></> : <><strong>Chưa chọn file</strong><span>Chọn một file ở danh sách bên trái.</span></>}</div>{file && <span>{formatBytes(file.bytes)} · {formatDate(file.modifiedAt)}</span>}</header>
                {loadingFile ? <div className="source-editor-empty" role="status">Đang mở file…</div> : file ? <textarea aria-label={`Nội dung file ${file.name}`} value={content} disabled={saving} spellCheck={false} onChange={event => { setContent(event.target.value); setResult(null); setError(''); }}/>: <div className="source-editor-empty"><span><Icon name="file" size={27}/></span><strong>Chọn file để xem nội dung</strong><p>File mật, binary, liên kết và dependency được khóa tự động.</p></div>}
                <footer>
                    <div className={dirty ? 'source-edit-state dirty' : 'source-edit-state'}><span/>{dirty ? 'Có thay đổi chưa lưu' : file ? 'Chưa có thay đổi' : 'Không có file đang mở'}</div>
                    <div><button className="button secondary" disabled={!dirty || saving} onClick={() => { if (file) setContent(file.content); setResult(null); setLeavePending(false); }}>Hoàn tác</button><button className="button primary" disabled={!dirty || saving} onClick={() => void saveFile()}>{saving ? 'Đang lưu…' : 'Lưu source'}</button></div>
                </footer>
                {result && <div className={result.success ? 'source-save-result success' : 'source-save-result error'} role="status"><Icon name={result.success ? 'check' : 'warning'} size={17}/><div><strong>{result.message}</strong><p>{result.success ? 'Đã tạo bản dự phòng trước khi thay file gốc.' : result.detail || 'File gốc chưa bị ghi đè.'}</p></div>{!result.success && file && <button className="button tertiary" onClick={() => void reloadFile()}>Mở lại file</button>}</div>}
            </section>
        </div>
    </section>;
}

function ProjectDetail({project, report, environment, readyDomainCount, environmentPlanning, copyPlanning, runtimePlanning, databasePlanning, tunnelPlanning, stoppingTunnel, projectData, projectDataLoading, backupProgress, backupResult, backingUp, onBack, onEdit, onCreateEnvironment, onCopyWebsite, onInstallRuntime, onImportDatabase, onStartTunnel, onStopTunnel, onOpenURL, onOpenSource, onCreateBackup, onOpenBackup, onCloudflareSetup, onSetup}: {
    project: StoredProject;
    report: ReadinessReport | null;
    environment: EnvironmentResult | null;
    readyDomainCount: number;
    environmentPlanning: boolean;
    copyPlanning: boolean;
    runtimePlanning: boolean;
    databasePlanning: boolean;
    tunnelPlanning: boolean;
    stoppingTunnel: boolean;
    projectData: ProjectDataStatus | null;
    projectDataLoading: boolean;
    backupProgress: BackupProgress | null;
    backupResult: BackupResult | null;
    backingUp: boolean;
    onBack: () => void;
    onEdit: () => void;
    onCreateEnvironment: () => Promise<void>;
    onCopyWebsite: () => Promise<void>;
    onInstallRuntime: () => Promise<void>;
    onImportDatabase: () => Promise<void>;
    onStartTunnel: () => void;
    onStopTunnel: () => Promise<void>;
    onOpenURL: (url: string) => Promise<void>;
    onOpenSource: () => void;
    onCreateBackup: () => Promise<void>;
    onOpenBackup: () => Promise<void>;
    onCloudflareSetup: () => void;
    onSetup: () => void;
}) {
    const state = stageCopy(project);
    const completed = completedDeploymentSteps(project.stage);
    const steps = ['Ubuntu', 'Source', 'Vùng chạy', 'Database', 'Domain'];

    return <section className="content-stack website-detail-page" aria-labelledby="project-detail-title">
        <div className="detail-toolbar"><button className="button secondary" onClick={onBack}><Icon name="back" size={17}/>Danh sách website</button></div>

        <article className="project-detail-card">
            <span className="project-detail-icon"><Icon name="globe" size={22}/></span>
            <div className="project-detail-copy">
                <div className="project-detail-title"><h2 id="project-detail-title">{project.name}</h2><span className={`project-status ${state.tone}`}>{state.label}</span></div>
                <p title={project.path}>{project.path}</p>
                <div className="project-detail-meta"><span>{project.kindLabel}</span><span>{project.phpVersion === 'auto' ? 'PHP 8.3 tự động' : `PHP ${project.phpVersion}`}</span><span>Thư mục chạy: {project.documentRoot}</span></div>
            </div>
            <button className="button tertiary" onClick={onEdit}>Chỉnh sửa cấu hình</button>
        </article>

        <ol className="deployment-progress" aria-label="Tiến trình triển khai website">
            {steps.map((label, index) => {
                const stepState = index < completed ? 'complete' : index === completed ? 'current' : 'pending';
                return <li key={label} className={stepState} aria-current={stepState === 'current' ? 'step' : undefined}><span>{stepState === 'complete' ? <Icon name="check" size={14}/> : index + 1}</span><div><strong>{label}</strong><small>{stepState === 'complete' ? 'Hoàn tất' : stepState === 'current' ? 'Tiếp theo' : 'Chưa thực hiện'}</small></div></li>;
            })}
        </ol>

        <section className="resume-panel" aria-label="Bước tiếp theo của website">
            <header><div><strong>Triển khai website</strong><span>{state.label} · cập nhật {formatDate(project.updatedAt)}</span></div></header>
            {!report?.ready ? <div className="next-step-card"><span className="next-step-icon warning"><Icon name="warning" size={19}/></span><div><strong>Chưa thể tiếp tục</strong><p>Hoàn tất thành phần còn thiếu trong Kiểm tra máy.</p></div><button className="button primary" onClick={onSetup}>Kiểm tra máy</button></div>
                : !environment ? <div className="next-step-card"><span className="next-step-icon"><Icon name="shield" size={19}/></span><div><strong>{project.stage === 'environment_failed' ? 'Thử lại: Chuẩn bị Ubuntu' : 'Bước tiếp theo: Chuẩn bị Ubuntu'}</strong><p>{project.lastError ? shortText(project.lastError, 110) : 'Một máy Ubuntu dùng chung; chưa sao chép website.'}</p></div><button className="button primary" disabled={environmentPlanning} onClick={() => void onCreateEnvironment()}>{environmentPlanning ? 'Đang kiểm tra…' : 'Chuẩn bị Ubuntu'}<Icon name="arrow" size={16}/></button></div>
                : project.stage === 'public' ? <div className="next-step-card tunnel-public"><span className="next-step-icon"><Icon name="globe" size={19}/></span><div><strong>Website đang public</strong><p title={project.tunnelUrl}>{project.tunnelUrl}</p></div><div className="next-step-actions"><button className="button secondary" disabled={stoppingTunnel} onClick={() => void onStopTunnel()}>{stoppingTunnel ? 'Đang dừng…' : 'Dừng truy cập'}</button><button className="button primary" onClick={() => void onOpenURL(project.tunnelUrl || '')}>Mở website</button></div></div>
                : project.stage === 'tunnel_stop_failed' ? <div className="next-step-card copy-failed"><span className="next-step-icon warning"><Icon name="warning" size={19}/></span><div><strong>Chưa xác nhận đã dừng sạch domain</strong><p>{project.lastError ? shortText(project.lastError, 110) : 'Thử dừng lại để xóa DNS và connector.'}</p></div><button className="button danger" disabled={stoppingTunnel} onClick={() => void onStopTunnel()}>{stoppingTunnel ? 'Đang dừng…' : 'Thử dừng lại'}</button></div>
                : project.stage === 'database_ready' || project.stage === 'tunnel_failed' ? <div className={project.stage === 'tunnel_failed' ? 'next-step-card copy-failed' : 'next-step-card environment-ready'}><span className={project.stage === 'tunnel_failed' ? 'next-step-icon warning' : 'next-step-icon'}><Icon name={project.stage === 'tunnel_failed' ? 'warning' : 'check'} size={19}/></span><div><strong>{project.stage === 'tunnel_failed' ? 'Thử lại: Kết nối domain' : 'Source và database sẵn sàng'}</strong><p>{project.lastError ? shortText(project.lastError, 110) : readyDomainCount > 0 ? `${readyDomainCount} domain sẵn sàng; chọn domain cho website này.` : 'Thêm domain Cloudflare để xuất bản.'}</p></div>{readyDomainCount > 0 ? <button className="button primary" disabled={tunnelPlanning} onClick={onStartTunnel}>{tunnelPlanning ? 'Đang kiểm tra…' : 'Chọn domain'}<Icon name="arrow" size={16}/></button> : <button className="button primary" onClick={onCloudflareSetup}>Cài đặt domain</button>}</div>
                : project.stage === 'runtime_ready' || project.stage === 'database_failed' ? <div className={project.stage === 'database_failed' ? 'next-step-card copy-failed' : 'next-step-card environment-ready'}><span className={project.stage === 'database_failed' ? 'next-step-icon warning' : 'next-step-icon'}><Icon name={project.stage === 'database_failed' ? 'warning' : 'shield'} size={19}/></span><div><strong>{project.stage === 'database_failed' ? 'Thử lại: Sao chép database' : 'Bước tiếp theo: Sao chép database'}</strong><p>{project.lastError ? shortText(project.lastError, 110) : 'Tự lấy database WordPress hoặc chọn file .sql/.sql.gz.'}</p></div><button className="button primary" disabled={databasePlanning} onClick={() => void onImportDatabase()}>{databasePlanning ? 'Đang kiểm tra…' : 'Sao chép database'}<Icon name="arrow" size={16}/></button></div>
                : project.stage === 'source_ready' || project.stage === 'runtime_failed' ? <div className={project.stage === 'runtime_failed' ? 'next-step-card copy-failed' : 'next-step-card environment-ready'}><span className={project.stage === 'runtime_failed' ? 'next-step-icon warning' : 'next-step-icon'}><Icon name={project.stage === 'runtime_failed' ? 'warning' : 'shield'} size={19}/></span><div><strong>{project.stage === 'runtime_failed' ? 'Thử lại: Tạo vùng chạy riêng' : 'Bước tiếp theo: Tạo vùng chạy riêng'}</strong><p>{project.lastError ? shortText(project.lastError, 110) : 'User, PHP sandbox và database riêng trên Ubuntu dùng chung.'}</p></div><button className="button primary" disabled={runtimePlanning} onClick={() => void onInstallRuntime()}>{runtimePlanning ? 'Đang kiểm tra…' : 'Tạo vùng chạy'}<Icon name="arrow" size={16}/></button></div>
                : <div className={project.stage === 'copy_failed' ? 'next-step-card copy-failed' : 'next-step-card environment-ready'}><span className={project.stage === 'copy_failed' ? 'next-step-icon warning' : 'next-step-icon'}><Icon name={project.stage === 'copy_failed' ? 'warning' : 'shield'} size={19}/></span><div><strong>{project.stage === 'copy_failed' ? 'Thử lại: Sao chép website' : 'Bước tiếp theo: Sao chép website'}</strong><p>{project.lastError ? shortText(project.lastError, 110) : 'Tạo snapshot một chiều; không chỉnh sửa source trên máy.'}</p></div><button className="button primary" disabled={copyPlanning} onClick={() => void onCopyWebsite()}>{copyPlanning ? 'Đang kiểm tra…' : 'Sao chép website'}<Icon name="arrow" size={16}/></button></div>}
            <ProjectDataPanel project={project} data={projectData} loading={projectDataLoading} progress={backupProgress} result={backupResult} backingUp={backingUp} databasePlanning={databasePlanning} onOpenSource={onOpenSource} onCreateBackup={onCreateBackup} onOpenBackup={onOpenBackup} onReplaceDatabase={onImportDatabase}/>
            <ProjectHistory project={project}/>
        </section>
    </section>;
}

function ProjectDataPanel({project, data, loading, progress, result, backingUp, databasePlanning, onOpenSource, onCreateBackup, onOpenBackup, onReplaceDatabase}: {
    project: StoredProject;
    data: ProjectDataStatus | null;
    loading: boolean;
    progress: BackupProgress | null;
    result: BackupResult | null;
    backingUp: boolean;
    databasePlanning: boolean;
    onOpenSource: () => void;
    onCreateBackup: () => Promise<void>;
    onOpenBackup: () => Promise<void>;
    onReplaceDatabase: () => Promise<void>;
}) {
    if (loading) {
        return <section className="project-data-panel" aria-label="Source và database"><header><div><Icon name="archive" size={18}/><strong>Source và database</strong></div><span>Đang đọc dữ liệu…</span></header></section>;
    }

    const backupLabel = data?.backupCount ? `${formatNumber(data.backupCount)} bản · gần nhất ${formatDate(data.lastBackupAt || '')}` : 'Chưa có backup';
    const databaseLabel = data?.database ? `${data.database} · ${formatNumber(data.databaseTables || project.databaseTables || 0)} bảng` : 'Chưa có database đã deploy';
    const replaceLabel = data?.canReplaceDatabase ? 'Thay database' : project.stage === 'public' ? 'Dừng public để thay' : 'Chưa thể thay';
    const replaceHint = data?.canReplaceDatabase ? 'Nhập file SQL vào database của website này' : project.stage === 'public' ? 'Dừng truy cập domain trước khi thay database' : !data?.backupCount ? 'Tạo backup trước khi thay database' : 'Database chưa ở trạng thái có thể thay';

    return <section className="project-data-panel" aria-labelledby="project-data-title">
        <header><div><Icon name="archive" size={18}/><strong id="project-data-title">Source và database</strong></div><span>{backupLabel}</span></header>
        <div className="project-data-row">
            <span className="project-data-icon"><Icon name="folder" size={18}/></span>
            <div className="project-data-copy"><strong>Source gốc</strong><code title={data?.sourcePath || project.path}>{data?.sourcePath || project.path}</code><p>Bản đang chạy được cách ly trong Ubuntu. Chỉnh source gốc rồi cập nhật website ở bước tiếp theo.</p></div>
            <button className="button secondary" onClick={onOpenSource}>Quản lý source</button>
        </div>
        <div className="project-data-row">
            <span className="project-data-icon"><Icon name="database" size={18}/></span>
            <div className="project-data-copy"><strong>Database đã deploy</strong><code title={databaseLabel}>{databaseLabel}</code><p>Tạo backup để xem/chỉnh file database.sql. File wp-config.php và mật khẩu runtime không được xuất.</p></div>
            <div className="project-data-actions">
                <button className="button tertiary" disabled={!data?.canBackup || backingUp} onClick={() => void onCreateBackup()}>{backingUp ? 'Đang backup…' : 'Tạo backup'}</button>
                <button className="button secondary" disabled={!data?.backupCount || backingUp} onClick={() => void onOpenBackup()}>Mở backup</button>
                <button className="button secondary" disabled={!data?.canReplaceDatabase || databasePlanning || backingUp} title={replaceHint} onClick={() => void onReplaceDatabase()}>{databasePlanning ? 'Đang kiểm tra…' : replaceLabel}</button>
            </div>
        </div>
        {backingUp && progress && <div className="project-data-progress" role="status" aria-live="polite"><div className="summary-progress"><span style={{width: `${progress.percent}%`}}/></div><div><strong>{progress.message}</strong><span>{progress.percent}%</span></div></div>}
        {!backingUp && result && <div className={result.success ? 'project-data-result success' : 'project-data-result error'} role="status"><Icon name={result.success ? 'check' : 'warning'} size={17}/><div><strong>{result.message}</strong>{result.success ? <p>{formatBytes(result.sourceBytes || 0)} source · {formatBytes(result.databaseBytes || 0)} database</p> : <p>Website đang chạy không bị thay đổi.</p>}{result.detail && <details className="technical-error"><summary>Chi tiết kỹ thuật</summary><pre>{shortText(result.detail, 1200)}</pre></details>}</div></div>}
    </section>;
}

function completedDeploymentSteps(stage: string): number {
    if (stage === 'public') return 5;
    if (['database_ready', 'tunnel_starting', 'tunnel_failed', 'tunnel_stop_failed', 'tunnel_stopping'].includes(stage)) return 4;
    if (['runtime_ready', 'database_importing', 'database_failed'].includes(stage)) return 3;
    if (['source_ready', 'runtime_installing', 'runtime_failed'].includes(stage)) return 2;
    if (['environment_ready', 'copying', 'copy_failed'].includes(stage)) return 1;
    return 0;
}

function DetectedProjectRow({info, disabled, onOpen}: {info: ProjectInfo; disabled: boolean; onOpen: () => void}) {
    return <article className="project-card detected-project">
        <span className="project-icon"><Icon name="folder"/></span>
        <div><h3>{info.name}</h3><p title={info.path}>{info.path}</p><small>{info.kindLabel} · Tự động nhận diện</small></div>
        <span className="project-status running">Chưa thiết lập</span>
        <button className="button primary" disabled={disabled} onClick={onOpen}>Thiết lập</button>
    </article>;
}

function ProjectRow({project, onOpen}: {project: StoredProject; onOpen: () => void}) {
    const state = stageCopy(project);
    return <article className="project-card">
        <span className="project-icon"><Icon name="globe"/></span>
        <div><h3>{project.name}</h3><p title={project.path}>{project.path}</p><small>{project.kindLabel} · {formatDate(project.updatedAt)}</small></div>
        <span className={`project-status ${state.tone}`}>{state.label}</span>
        <button className="button secondary" aria-label={`Xem chi tiết ${project.name}`} onClick={onOpen}>Chi tiết</button>
    </article>;
}

function ProjectHistory({project}: {project: StoredProject}) {
    const entries = [...(project.history || [])].reverse().slice(0, 5);
    return <details className="project-history"><summary><Icon name="history" size={16}/>Lịch sử gần đây <span>{project.history?.length || 0}</span></summary>
        <div className="history-list">{entries.map(entry => <div key={entry.id}><span className={`history-dot ${entry.status}`}/><div><strong>{entry.message}</strong><p>{formatDate(entry.createdAt)}{entry.detail ? ` · ${shortText(entry.detail, 150)}` : ''}</p></div></div>)}</div>
    </details>;
}

function ProjectConfiguration({info, settings, onChange, onBack, onConfirm}: {
    info: ProjectInfo;
    settings: ProjectSettings;
    onChange: (settings: ProjectSettings) => void;
    onBack: () => void;
    onConfirm: (settings: ProjectSettings) => Promise<void>;
}) {
    const [error, setError] = useState('');
    const [saving, setSaving] = useState(false);

    const update = (field: keyof ProjectSettings, value: string) => {
        const next = {...settings, [field]: value} as ProjectSettings;
        if (field === 'kind' && value === 'static') next.phpVersion = 'none';
        if (field === 'kind' && value !== 'static' && next.phpVersion === 'none') next.phpVersion = 'auto';
        setError('');
        onChange(next);
    };

    const submit = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        if (!settings.name.trim()) {
            setError('Nhập tên website.');
            return;
        }
        if (settings.kind === 'unknown') {
            setError('Chọn loại website.');
            return;
        }
        if (!settings.documentRoot.trim()) {
            setError('Nhập thư mục chạy website.');
            return;
        }
        const root = settings.documentRoot.trim().replaceAll('\\', '/');
        if (root.startsWith('/') || /^[a-zA-Z]:/.test(root) || root.split('/').includes('..')) {
            setError('Thư mục chạy phải nằm bên trong website đã chọn.');
            return;
        }
        setSaving(true);
        try {
            await onConfirm({...settings, name: settings.name.trim(), documentRoot: settings.documentRoot.trim()});
        } catch (saveError) {
            setError(`Không lưu được cấu hình: ${cleanError(saveError)}`);
        } finally {
            setSaving(false);
        }
    };

    return <section className="configuration-page">
        <ol className="wizard-steps" aria-label="Tiến trình cấu hình">
            <li className="complete"><span><Icon name="check" size={14}/></span><div><strong>Chọn website</strong><small>Hoàn tất</small></div></li>
            <li className="active" aria-current="step"><span>2</span><div><strong>Cấu hình</strong><small>Đang thực hiện</small></div></li>
            <li><span>3</span><div><strong>Tạo môi trường</strong><small>Bước tiếp theo</small></div></li>
        </ol>

        <form className="configuration-form" onSubmit={event => void submit(event)}>
            <header className="form-header"><div><h2>Thông tin website</h2><p>{info.path}</p></div><span className="detected-badge">Đã nhận diện: {info.kindLabel}</span></header>
            {info.warnings.length > 0 && <div className="inline-warning" role="status"><Icon name="warning" size={17}/><span>{info.warnings.join(' ')}</span></div>}
            {error && <div className="inline-error" role="alert"><Icon name="close" size={17}/><span>{error}</span></div>}

            <div className="form-grid">
                <label className="field"><span>Tên website</span><input value={settings.name} onChange={event => update('name', event.target.value)} autoFocus/></label>
                <label className="field"><span>Loại website</span><select value={settings.kind} onChange={event => update('kind', event.target.value)}><option value="unknown">Chọn loại website</option><option value="laravel">Laravel</option><option value="wordpress">WordPress</option><option value="php-framework">PHP Framework</option><option value="php">PHP</option><option value="static">Website tĩnh</option></select></label>
                <label className="field"><span>Thư mục chạy</span><input value={settings.documentRoot} onChange={event => update('documentRoot', event.target.value)}/><small>Dùng “.” nếu chạy từ thư mục gốc.</small></label>
                {settings.kind !== 'static' && <label className="field"><span>Phiên bản PHP</span><select value={settings.phpVersion} onChange={event => update('phpVersion', event.target.value)}><option value="auto">Tự động (PHP 8.3)</option><option value="8.3">PHP 8.3</option></select>{info.phpRequirement && <small>composer.json yêu cầu: {info.phpRequirement}</small>}</label>}
                <div className="field read-only-field"><span>Kiểu truy cập</span><div>Domain riêng (Cloudflare Tunnel)</div><small>Domain và subdomain được chọn sau khi runtime sẵn sàng.</small></div>
            </div>

            <footer className="form-actions"><button type="button" className="button secondary" disabled={saving} onClick={onBack}><Icon name="back" size={17}/>Quay lại</button><div><span>Cấu hình sẽ được tự động lưu trên máy.</span><button type="submit" className="button primary" disabled={saving}>{saving ? 'Đang lưu…' : 'Lưu cấu hình'}<Icon name="arrow" size={17}/></button></div></footer>
        </form>
    </section>;
}

function stageCopy(project: StoredProject): {label: string; tone: string} {
    switch (project.stage) {
        case 'public': return {label: 'Đang public', tone: 'ready'};
        case 'tunnel_starting': return {label: 'Đang kết nối domain', tone: 'running'};
        case 'tunnel_stopping': return {label: 'Đang dừng domain', tone: 'running'};
        case 'tunnel_failed': return {label: 'Domain lỗi', tone: 'error'};
        case 'tunnel_stop_failed': return {label: 'Cần kiểm tra domain', tone: 'error'};
        case 'database_ready': return {label: 'Database sẵn sàng', tone: 'ready'};
        case 'database_importing': return {label: 'Đang sao chép database', tone: 'running'};
        case 'database_failed': return {label: 'Database lỗi', tone: 'error'};
        case 'runtime_ready': return {label: 'Runtime sẵn sàng', tone: 'ready'};
        case 'runtime_installing': return {label: 'Đang cài runtime', tone: 'running'};
        case 'runtime_failed': return {label: 'Runtime lỗi', tone: 'error'};
        case 'source_ready': return {label: 'Đã sao chép', tone: 'ready'};
        case 'copying': return {label: 'Đang sao chép', tone: 'running'};
        case 'copy_failed': return {label: 'Sao chép lỗi', tone: 'error'};
        case 'environment_ready': return {label: 'Môi trường sẵn sàng', tone: 'ready'};
        case 'environment_creating': return {label: 'Đang tạo môi trường', tone: 'running'};
        case 'environment_failed': return {label: 'Cần thử lại', tone: 'error'};
        default: return {label: 'Đã cấu hình', tone: 'configured'};
    }
}

function hasEnvironment(stage: string): boolean {
    return stage === 'environment_ready' || stage === 'copying' || stage === 'copy_failed' || stage === 'source_ready' || stage === 'runtime_installing' || stage === 'runtime_failed' || stage === 'runtime_ready' || stage === 'database_importing' || stage === 'database_failed' || stage === 'database_ready' || stage === 'tunnel_starting' || stage === 'tunnel_failed' || stage === 'public' || stage === 'tunnel_stopping' || stage === 'tunnel_stop_failed';
}

function samePath(left: string, right: string): boolean {
	if (left === right) return true;
	const windowsPath = /^[a-zA-Z]:[\\/]/;
	if (!windowsPath.test(left) || !windowsPath.test(right)) return false;
	return left.replaceAll('/', '\\').toLocaleLowerCase() === right.replaceAll('/', '\\').toLocaleLowerCase();
}

function formatDate(value: string): string {
    if (!value) return 'chưa cập nhật';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return new Intl.DateTimeFormat('vi-VN', {day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit'}).format(date);
}

function formatNumber(value: number): string {
    return new Intl.NumberFormat('vi-VN').format(value);
}

function formatBytes(value: number): string {
    if (!Number.isFinite(value) || value <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB'];
    const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
    const amount = value / Math.pow(1024, index);
    return `${new Intl.NumberFormat('vi-VN', {maximumFractionDigits: index === 0 ? 0 : 1}).format(amount)} ${units[index]}`;
}

function suggestedRecoveryPath(host: string): string {
    const normalized = host.trim().toLowerCase().replace(/\.$/, '').replace(/^ftp\./, '');
    if (!normalized || normalized.includes(':') || !/^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$/.test(normalized) || !normalized.includes('.')) return '';
    return `/domains/${normalized}/public_html`;
}

function shortText(value: string, max: number): string {
    const clean = value.replace(/\x1B\[[0-?]*[ -/]*[@-~]/g, '').replace(/(?:[\\/|\-]){8,}/g, '').replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\uFFFD]/g, '').replace(/\s+/g, ' ').trim();
    return clean.length > max ? `${clean.slice(0, max)}…` : clean;
}

function slugify(value: string): string {
    return value.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40);
}

function cleanError(error: unknown): string {
    return shortText(String(error).replace(/^Error:\s*/i, ''), 500);
}

export default App;
