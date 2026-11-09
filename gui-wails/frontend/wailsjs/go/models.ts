export namespace main {
	
	export class HostOpResult {
	    applied: boolean;
	    host: string;
	
	    static createFrom(source: any = {}) {
	        return new HostOpResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.applied = source["applied"];
	        this.host = source["host"];
	    }
	}
	export class LogEntry {
	    time: string;
	    level: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.level = source["level"];
	        this.text = source["text"];
	    }
	}
	export class MapRule {
	    host?: string;
	    remote: number;
	    local: string;
	
	    static createFrom(source: any = {}) {
	        return new MapRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.remote = source["remote"];
	        this.local = source["local"];
	    }
	}
	export class UserInfo {
	    id: number;
	    username: string;
	    role: string;
	    tenant_id: number;
	    tenant: string;
	
	    static createFrom(source: any = {}) {
	        return new UserInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.username = source["username"];
	        this.role = source["role"];
	        this.tenant_id = source["tenant_id"];
	        this.tenant = source["tenant"];
	    }
	}
	export class LoginResult {
	    platform_url: string;
	    platform_ws: string;
	    user: UserInfo;
	    device_uid: string;
	    device_name: string;
	    device_role: string;
	    mappings?: MapRule[];
	    hosts?: string[];
	
	    static createFrom(source: any = {}) {
	        return new LoginResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform_url = source["platform_url"];
	        this.platform_ws = source["platform_ws"];
	        this.user = this.convertValues(source["user"], UserInfo);
	        this.device_uid = source["device_uid"];
	        this.device_name = source["device_name"];
	        this.device_role = source["device_role"];
	        this.mappings = this.convertValues(source["mappings"], MapRule);
	        this.hosts = source["hosts"];
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
	
	export class MappingOpResult {
	    applied: boolean;
	    rule: MapRule;
	
	    static createFrom(source: any = {}) {
	        return new MappingOpResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.applied = source["applied"];
	        this.rule = this.convertValues(source["rule"], MapRule);
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
	export class SavedRules {
	    rules: MapRule[];
	    hosts: string[];
	
	    static createFrom(source: any = {}) {
	        return new SavedRules(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rules = this.convertValues(source["rules"], MapRule);
	        this.hosts = source["hosts"];
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
	export class SessionState {
	    platform_url: string;
	    platform_ws: string;
	    user: UserInfo;
	    device_uid: string;
	    device_name: string;
	    device_role: string;
	    device_token: string;
	    mappings?: MapRule[];
	    hosts?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SessionState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform_url = source["platform_url"];
	        this.platform_ws = source["platform_ws"];
	        this.user = this.convertValues(source["user"], UserInfo);
	        this.device_uid = source["device_uid"];
	        this.device_name = source["device_name"];
	        this.device_role = source["device_role"];
	        this.device_token = source["device_token"];
	        this.mappings = this.convertValues(source["mappings"], MapRule);
	        this.hosts = source["hosts"];
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
	export class UpdateInfo {
	    has_update: boolean;
	    version: string;
	    notes: string;
	    current_version: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.has_update = source["has_update"];
	        this.version = source["version"];
	        this.notes = source["notes"];
	        this.current_version = source["current_version"];
	    }
	}
	
	export class loginHint {
	    platform?: string;
	    tenant?: string;
	    user?: string;
	    pass?: string;
	    dev_name?: string;
	    role?: string;
	
	    static createFrom(source: any = {}) {
	        return new loginHint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform = source["platform"];
	        this.tenant = source["tenant"];
	        this.user = source["user"];
	        this.pass = source["pass"];
	        this.dev_name = source["dev_name"];
	        this.role = source["role"];
	    }
	}

}

