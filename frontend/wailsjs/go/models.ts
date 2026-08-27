export namespace backup {
	
	export class Result {
	    success: boolean;
	    message: string;
	    detail?: string;
	    path?: string;
	    sourceBytes?: number;
	    databaseBytes?: number;
	    createdAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.path = source["path"];
	        this.sourceBytes = source["sourceBytes"];
	        this.databaseBytes = source["databaseBytes"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Status {
	    projectPath: string;
	    sourcePath: string;
	    deployedPath: string;
	    database: string;
	    databaseTables: number;
	    databaseImportedAt?: string;
	    backupRoot: string;
	    backupCount: number;
	    lastBackupAt?: string;
	    lastBackupPath?: string;
	    canBackup: boolean;
	    canReplaceDatabase: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	        this.sourcePath = source["sourcePath"];
	        this.deployedPath = source["deployedPath"];
	        this.database = source["database"];
	        this.databaseTables = source["databaseTables"];
	        this.databaseImportedAt = source["databaseImportedAt"];
	        this.backupRoot = source["backupRoot"];
	        this.backupCount = source["backupCount"];
	        this.lastBackupAt = source["lastBackupAt"];
	        this.lastBackupPath = source["lastBackupPath"];
	        this.canBackup = source["canBackup"];
	        this.canReplaceDatabase = source["canReplaceDatabase"];
	        this.message = source["message"];
	    }
	}

}

export namespace cloudflare {
	
	export class ConnectionInput {
	    zone: string;
	    apiToken: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.zone = source["zone"];
	        this.apiToken = source["apiToken"];
	    }
	}
	export class ConnectionStatus {
	    connected: boolean;
	    tokenStored: boolean;
	    zone: string;
	    zoneId?: string;
	    accountId?: string;
	    zoneStatus?: string;
	    assignedNameServers?: string[];
	    activeNameServers?: string[];
	    dnsReady: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.tokenStored = source["tokenStored"];
	        this.zone = source["zone"];
	        this.zoneId = source["zoneId"];
	        this.accountId = source["accountId"];
	        this.zoneStatus = source["zoneStatus"];
	        this.assignedNameServers = source["assignedNameServers"];
	        this.activeNameServers = source["activeNameServers"];
	        this.dnsReady = source["dnsReady"];
	        this.message = source["message"];
	    }
	}

}

export namespace database {
	
	export class Plan {
	    projectPath: string;
	    mode: string;
	    ready: boolean;
	    source: string;
	    database?: string;
	    tablePrefix?: string;
	    tool?: string;
	    importPath?: string;
	    importBytes?: number;
	    automaticMessage?: string;
	    replacementNotice: string;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	        this.mode = source["mode"];
	        this.ready = source["ready"];
	        this.source = source["source"];
	        this.database = source["database"];
	        this.tablePrefix = source["tablePrefix"];
	        this.tool = source["tool"];
	        this.importPath = source["importPath"];
	        this.importBytes = source["importBytes"];
	        this.automaticMessage = source["automaticMessage"];
	        this.replacementNotice = source["replacementNotice"];
	    }
	}
	export class Request {
	    projectPath: string;
	    mode: string;
	    importPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	        this.mode = source["mode"];
	        this.importPath = source["importPath"];
	    }
	}
	export class Result {
	    success: boolean;
	    message: string;
	    detail?: string;
	    source?: string;
	    tables?: number;
	    bytes?: number;
	    tablePrefix?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.source = source["source"];
	        this.tables = source["tables"];
	        this.bytes = source["bytes"];
	        this.tablePrefix = source["tablePrefix"];
	    }
	}

}

export namespace datastore {
	
	export class Status {
	    platform: string;
	    supported: boolean;
	    configured: boolean;
	    locked: boolean;
	    canConfigure: boolean;
	    requiresAdmin: boolean;
	    currentPath: string;
	    defaultPath: string;
	    recommendedPath: string;
	    freeBytes: number;
	    instanceCount: number;
	    projectCount: number;
	    message: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform = source["platform"];
	        this.supported = source["supported"];
	        this.configured = source["configured"];
	        this.locked = source["locked"];
	        this.canConfigure = source["canConfigure"];
	        this.requiresAdmin = source["requiresAdmin"];
	        this.currentPath = source["currentPath"];
	        this.defaultPath = source["defaultPath"];
	        this.recommendedPath = source["recommendedPath"];
	        this.freeBytes = source["freeBytes"];
	        this.instanceCount = source["instanceCount"];
	        this.projectCount = source["projectCount"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	    }
	}
	export class Result {
	    success: boolean;
	    message: string;
	    detail?: string;
	    status: Status;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.status = this.convertValues(source["status"], Status);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace installer {
	
	export class Plan {
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
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.publisher = source["publisher"];
	        this.version = source["version"];
	        this.source = source["source"];
	        this.downloadSize = source["downloadSize"];
	        this.requiresAdmin = source["requiresAdmin"];
	        this.rebootPossible = source["rebootPossible"];
	        this.supported = source["supported"];
	    }
	}
	export class Result {
	    success: boolean;
	    rebootRequired: boolean;
	    message: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.rebootRequired = source["rebootRequired"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	    }
	}

}

export namespace project {
	
	export class CopyPlan {
	    projectPath: string;
	    fileCount: number;
	    totalBytes: number;
	    excludedCount: number;
	    secretExcluded: number;
	    policy: string;
	
	    static createFrom(source: any = {}) {
	        return new CopyPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	        this.fileCount = source["fileCount"];
	        this.totalBytes = source["totalBytes"];
	        this.excludedCount = source["excludedCount"];
	        this.secretExcluded = source["secretExcluded"];
	        this.policy = source["policy"];
	    }
	}
	export class CopyRequest {
	    projectPath: string;
	
	    static createFrom(source: any = {}) {
	        return new CopyRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	    }
	}
	export class CopyResult {
	    success: boolean;
	    message: string;
	    detail?: string;
	    snapshotId?: string;
	    checksum?: string;
	    fileCount?: number;
	    totalBytes?: number;
	    guestPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new CopyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.snapshotId = source["snapshotId"];
	        this.checksum = source["checksum"];
	        this.fileCount = source["fileCount"];
	        this.totalBytes = source["totalBytes"];
	        this.guestPath = source["guestPath"];
	    }
	}
	export class Info {
	    path: string;
	    name: string;
	    kind: string;
	    kindLabel: string;
	    documentRoot: string;
	    phpRequirement?: string;
	    ready: boolean;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.kindLabel = source["kindLabel"];
	        this.documentRoot = source["documentRoot"];
	        this.phpRequirement = source["phpRequirement"];
	        this.ready = source["ready"];
	        this.warnings = source["warnings"];
	    }
	}

}

export namespace readiness {
	
	export class Check {
	    id: string;
	    title: string;
	    status: string;
	    summary: string;
	    detail?: string;
	    actionLabel?: string;
	    actionKind?: string;
	    required: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Check(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.summary = source["summary"];
	        this.detail = source["detail"];
	        this.actionLabel = source["actionLabel"];
	        this.actionKind = source["actionKind"];
	        this.required = source["required"];
	    }
	}
	export class Report {
	    platform: string;
	    arch: string;
	    ready: boolean;
	    // Go type: time
	    checkedAt: any;
	    checks: Check[];
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform = source["platform"];
	        this.arch = source["arch"];
	        this.ready = source["ready"];
	        this.checkedAt = this.convertValues(source["checkedAt"], null);
	        this.checks = this.convertValues(source["checks"], Check);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace recovery {
	
	export class BackupPlan {
	    projectId: string;
	    name: string;
	    host: string;
	    protocol: string;
	    remotePath: string;
	    destination: string;
	    workers: number;
	    readOnly: boolean;
	    storageMode: string;
	
	    static createFrom(source: any = {}) {
	        return new BackupPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.protocol = source["protocol"];
	        this.remotePath = source["remotePath"];
	        this.destination = source["destination"];
	        this.workers = source["workers"];
	        this.readOnly = source["readOnly"];
	        this.storageMode = source["storageMode"];
	    }
	}
	export class BackupResult {
	    success: boolean;
	    message: string;
	    detail?: string;
	    backupId?: string;
	    path?: string;
	    manifestPath?: string;
	    reportPath?: string;
	    files?: number;
	    bytes?: number;
	    findings?: number;
	    critical?: number;
	    createdAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new BackupResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.backupId = source["backupId"];
	        this.path = source["path"];
	        this.manifestPath = source["manifestPath"];
	        this.reportPath = source["reportPath"];
	        this.files = source["files"];
	        this.bytes = source["bytes"];
	        this.findings = source["findings"];
	        this.critical = source["critical"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class ConnectionResult {
	    success: boolean;
	    message: string;
	    detail?: string;
	    remotePath?: string;
	    secure: boolean;
	    checkedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.remotePath = source["remotePath"];
	        this.secure = source["secure"];
	        this.checkedAt = source["checkedAt"];
	    }
	}
	export class HistoryEntry {
	    id: string;
	    action: string;
	    status: string;
	    message: string;
	    detail?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.action = source["action"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Project {
	    id: string;
	    name: string;
	    host: string;
	    username: string;
	    protocol: string;
	    port: number;
	    remotePath: string;
	    siteUrl?: string;
	    workers: number;
	    passive: boolean;
	    status: string;
	    lastError?: string;
	    lastCheckedAt?: string;
	    lastBackupId?: string;
	    backupFiles?: number;
	    backupBytes?: number;
	    findingCount?: number;
	    criticalCount?: number;
	    backupCreatedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	    history: HistoryEntry[];
	    passwordStored?: boolean;
	    dataPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.username = source["username"];
	        this.protocol = source["protocol"];
	        this.port = source["port"];
	        this.remotePath = source["remotePath"];
	        this.siteUrl = source["siteUrl"];
	        this.workers = source["workers"];
	        this.passive = source["passive"];
	        this.status = source["status"];
	        this.lastError = source["lastError"];
	        this.lastCheckedAt = source["lastCheckedAt"];
	        this.lastBackupId = source["lastBackupId"];
	        this.backupFiles = source["backupFiles"];
	        this.backupBytes = source["backupBytes"];
	        this.findingCount = source["findingCount"];
	        this.criticalCount = source["criticalCount"];
	        this.backupCreatedAt = source["backupCreatedAt"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.history = this.convertValues(source["history"], HistoryEntry);
	        this.passwordStored = source["passwordStored"];
	        this.dataPath = source["dataPath"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProjectInput {
	    id: string;
	    name: string;
	    host: string;
	    username: string;
	    password: string;
	    protocol: string;
	    port: number;
	    remotePath: string;
	    siteUrl: string;
	    workers: number;
	    passive: boolean;
	    allowInsecure: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProjectInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.protocol = source["protocol"];
	        this.port = source["port"];
	        this.remotePath = source["remotePath"];
	        this.siteUrl = source["siteUrl"];
	        this.workers = source["workers"];
	        this.passive = source["passive"];
	        this.allowInsecure = source["allowInsecure"];
	    }
	}
	export class Snapshot {
	    schema: number;
	    updatedAt: string;
	    storagePath: string;
	    projects: Project[];
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.updatedAt = source["updatedAt"];
	        this.storagePath = source["storagePath"];
	        this.projects = this.convertValues(source["projects"], Project);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace recoverytool {
	
	export class ImportPlan {
	    sourcePath: string;
	    destinationPath: string;
	    files: number;
	    bytes: number;
	    profiles: number;
	    secrets: number;
	    existingFiles: number;
	    conflicts: number;
	    ready: boolean;
	    message: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourcePath = source["sourcePath"];
	        this.destinationPath = source["destinationPath"];
	        this.files = source["files"];
	        this.bytes = source["bytes"];
	        this.profiles = source["profiles"];
	        this.secrets = source["secrets"];
	        this.existingFiles = source["existingFiles"];
	        this.conflicts = source["conflicts"];
	        this.ready = source["ready"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	    }
	}
	export class ImportResult {
	    success: boolean;
	    message: string;
	    detail?: string;
	    files: number;
	    bytes: number;
	    skipped: number;
	    destinationPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.files = source["files"];
	        this.bytes = source["bytes"];
	        this.skipped = source["skipped"];
	        this.destinationPath = source["destinationPath"];
	    }
	}
	export class Status {
	    state: string;
	    message: string;
	    detail?: string;
	    url?: string;
	    version?: string;
	    dataPath?: string;
	    ready: boolean;
	    running: boolean;
	    setupRequired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.url = source["url"];
	        this.version = source["version"];
	        this.dataPath = source["dataPath"];
	        this.ready = source["ready"];
	        this.running = source["running"];
	        this.setupRequired = source["setupRequired"];
	    }
	}

}

export namespace runtimeenv {
	
	export class Plan {
	    projectPath: string;
	    adapter: string;
	    runtime: string;
	    database: string;
	    services: number;
	    packages: string[];
	    download: string;
	    resources: string;
	    network: string;
	    public: boolean;
	    existing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	        this.adapter = source["adapter"];
	        this.runtime = source["runtime"];
	        this.database = source["database"];
	        this.services = source["services"];
	        this.packages = source["packages"];
	        this.download = source["download"];
	        this.resources = source["resources"];
	        this.network = source["network"];
	        this.public = source["public"];
	        this.existing = source["existing"];
	    }
	}
	export class Request {
	    projectPath: string;
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	    }
	}
	export class Result {
	    success: boolean;
	    message: string;
	    detail?: string;
	    adapter?: string;
	    health?: string;
	    services?: number;
	    snapshotId?: string;
	    runtimeState?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.adapter = source["adapter"];
	        this.health = source["health"];
	        this.services = source["services"];
	        this.snapshotId = source["snapshotId"];
	        this.runtimeState = source["runtimeState"];
	    }
	}

}

export namespace sourceeditor {
	
	export class Entry {
	    name: string;
	    path: string;
	    kind: string;
	    size?: number;
	    modifiedAt?: string;
	    editable: boolean;
	    blockedReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.kind = source["kind"];
	        this.size = source["size"];
	        this.modifiedAt = source["modifiedAt"];
	        this.editable = source["editable"];
	        this.blockedReason = source["blockedReason"];
	    }
	}
	export class File {
	    path: string;
	    name: string;
	    content: string;
	    sha256: string;
	    bytes: number;
	    modifiedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new File(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.content = source["content"];
	        this.sha256 = source["sha256"];
	        this.bytes = source["bytes"];
	        this.modifiedAt = source["modifiedAt"];
	    }
	}
	export class Listing {
	    projectPath: string;
	    directory: string;
	    parent: string;
	    entries: Entry[];
	
	    static createFrom(source: any = {}) {
	        return new Listing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	        this.directory = source["directory"];
	        this.parent = source["parent"];
	        this.entries = this.convertValues(source["entries"], Entry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SaveRequest {
	    projectPath: string;
	    path: string;
	    content: string;
	    expectedSha: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	        this.path = source["path"];
	        this.content = source["content"];
	        this.expectedSha = source["expectedSha"];
	    }
	}
	export class SaveResult {
	    success: boolean;
	    message: string;
	    detail?: string;
	    path?: string;
	    sha256?: string;
	    bytes?: number;
	    backupPath?: string;
	    savedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.path = source["path"];
	        this.sha256 = source["sha256"];
	        this.bytes = source["bytes"];
	        this.backupPath = source["backupPath"];
	        this.savedAt = source["savedAt"];
	    }
	}

}

export namespace state {
	
	export class CloudflareConnection {
	    zone?: string;
	    zoneId?: string;
	    accountId?: string;
	    zoneStatus?: string;
	    nameServers?: string[];
	    connectedAt?: string;
	    checkedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new CloudflareConnection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.zone = source["zone"];
	        this.zoneId = source["zoneId"];
	        this.accountId = source["accountId"];
	        this.zoneStatus = source["zoneStatus"];
	        this.nameServers = source["nameServers"];
	        this.connectedAt = source["connectedAt"];
	        this.checkedAt = source["checkedAt"];
	    }
	}
	export class HistoryEntry {
	    id: string;
	    action: string;
	    status: string;
	    message: string;
	    detail?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.action = source["action"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class ProjectInput {
	    path: string;
	    name: string;
	    kind: string;
	    documentRoot: string;
	    phpVersion: string;
	    tunnelMode: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.documentRoot = source["documentRoot"];
	        this.phpVersion = source["phpVersion"];
	        this.tunnelMode = source["tunnelMode"];
	    }
	}
	export class ProjectRecord {
	    id: string;
	    path: string;
	    name: string;
	    kind: string;
	    kindLabel: string;
	    documentRoot: string;
	    phpVersion: string;
	    tunnelMode: string;
	    stage: string;
	    status: string;
	    vmName?: string;
	    vmState?: string;
	    ip?: string;
	    snapshotId?: string;
	    snapshotHash?: string;
	    snapshotPath?: string;
	    snapshotFiles?: number;
	    snapshotBytes?: number;
	    runtimeAdapter?: string;
	    runtimeState?: string;
	    runtimeHealth?: string;
	    runtimeContainers?: number;
	    runtimeSnapshotId?: string;
	    databaseState?: string;
	    databaseSource?: string;
	    databaseTables?: number;
	    databaseBytes?: number;
	    databasePrefix?: string;
	    databaseImportedAt?: string;
	    domainZone?: string;
	    hostname?: string;
	    tunnelId?: string;
	    dnsRecordId?: string;
	    tunnelState?: string;
	    tunnelHealth?: string;
	    tunnelUrl?: string;
	    tunnelStartedAt?: string;
	    lastError?: string;
	    createdAt: string;
	    updatedAt: string;
	    history: HistoryEntry[];
	
	    static createFrom(source: any = {}) {
	        return new ProjectRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.kindLabel = source["kindLabel"];
	        this.documentRoot = source["documentRoot"];
	        this.phpVersion = source["phpVersion"];
	        this.tunnelMode = source["tunnelMode"];
	        this.stage = source["stage"];
	        this.status = source["status"];
	        this.vmName = source["vmName"];
	        this.vmState = source["vmState"];
	        this.ip = source["ip"];
	        this.snapshotId = source["snapshotId"];
	        this.snapshotHash = source["snapshotHash"];
	        this.snapshotPath = source["snapshotPath"];
	        this.snapshotFiles = source["snapshotFiles"];
	        this.snapshotBytes = source["snapshotBytes"];
	        this.runtimeAdapter = source["runtimeAdapter"];
	        this.runtimeState = source["runtimeState"];
	        this.runtimeHealth = source["runtimeHealth"];
	        this.runtimeContainers = source["runtimeContainers"];
	        this.runtimeSnapshotId = source["runtimeSnapshotId"];
	        this.databaseState = source["databaseState"];
	        this.databaseSource = source["databaseSource"];
	        this.databaseTables = source["databaseTables"];
	        this.databaseBytes = source["databaseBytes"];
	        this.databasePrefix = source["databasePrefix"];
	        this.databaseImportedAt = source["databaseImportedAt"];
	        this.domainZone = source["domainZone"];
	        this.hostname = source["hostname"];
	        this.tunnelId = source["tunnelId"];
	        this.dnsRecordId = source["dnsRecordId"];
	        this.tunnelState = source["tunnelState"];
	        this.tunnelHealth = source["tunnelHealth"];
	        this.tunnelUrl = source["tunnelUrl"];
	        this.tunnelStartedAt = source["tunnelStartedAt"];
	        this.lastError = source["lastError"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.history = this.convertValues(source["history"], HistoryEntry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Snapshot {
	    schema: number;
	    updatedAt: string;
	    storagePath: string;
	    domains: CloudflareConnection[];
	    projects: ProjectRecord[];
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.updatedAt = source["updatedAt"];
	        this.storagePath = source["storagePath"];
	        this.domains = this.convertValues(source["domains"], CloudflareConnection);
	        this.projects = this.convertValues(source["projects"], ProjectRecord);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace tunnel {
	
	export class Plan {
	    projectPath: string;
	    hostname: string;
	    provider: string;
	    mode: string;
	    address: string;
	    image: string;
	    download: string;
	    network: string;
	    exposure: string;
	    existing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	        this.hostname = source["hostname"];
	        this.provider = source["provider"];
	        this.mode = source["mode"];
	        this.address = source["address"];
	        this.image = source["image"];
	        this.download = source["download"];
	        this.network = source["network"];
	        this.exposure = source["exposure"];
	        this.existing = source["existing"];
	    }
	}
	export class Request {
	    projectPath: string;
	    hostname: string;
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	        this.hostname = source["hostname"];
	    }
	}
	export class Result {
	    success: boolean;
	    message: string;
	    detail?: string;
	    url?: string;
	    state?: string;
	    health?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.url = source["url"];
	        this.state = source["state"];
	        this.health = source["health"];
	    }
	}

}

export namespace vm {
	
	export class Plan {
	    vmName: string;
	    image: string;
	    cpus: number;
	    memory: string;
	    disk: string;
	    network: string;
	    isolation: string;
	    existing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.vmName = source["vmName"];
	        this.image = source["image"];
	        this.cpus = source["cpus"];
	        this.memory = source["memory"];
	        this.disk = source["disk"];
	        this.network = source["network"];
	        this.isolation = source["isolation"];
	        this.existing = source["existing"];
	    }
	}
	export class Request {
	    projectPath: string;
	    projectName: string;
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectPath = source["projectPath"];
	        this.projectName = source["projectName"];
	    }
	}
	export class Result {
	    success: boolean;
	    reused: boolean;
	    canRecreate?: boolean;
	    rebootRequired?: boolean;
	    message: string;
	    detail?: string;
	    vmName?: string;
	    state?: string;
	    ip?: string;
	    release?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.reused = source["reused"];
	        this.canRecreate = source["canRecreate"];
	        this.rebootRequired = source["rebootRequired"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.vmName = source["vmName"];
	        this.state = source["state"];
	        this.ip = source["ip"];
	        this.release = source["release"];
	    }
	}

}

