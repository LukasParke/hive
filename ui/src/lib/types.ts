// API types - extracted from api.ts

export interface SystemStatus {
	status: string;
	role: string;
	node_count: number;
	multi_node: boolean;
	nats: string;
}

export interface Project {
	id: string;
	name: string;
	org_id: string;
	description: string;
	created_at: string;
	updated_at: string;
}

export interface App {
	id: string;
	project_id: string;
	name: string;
	deploy_type: string;
	image: string;
	git_repo: string;
	git_branch: string;
	domain: string;
	port: number;
	replicas: number;
	status: string;
	cpu_limit: number;
	memory_limit: number;
	health_check_path: string;
	health_check_interval: number;
	homepage_labels: Record<string, string>;
	extra_labels: Record<string, string>;
	placement_constraints: string[];
	placement_preferences: string[];
	update_strategy: string;
	update_parallelism: number;
	update_delay: string;
	update_failure_action: string;
	update_order: string;
	created_at: string;
	updated_at: string;
	dns_status?: string;
	has_dns_provider?: boolean;
}

export interface CreateAppRequest {
	name: string;
	deploy_type: string;
	image?: string;
	git_repo?: string;
	git_branch?: string;
	dockerfile_path?: string;
	domain?: string;
	port?: number;
	replicas?: number;
}

export interface TaskInfo {
	id: string;
	node_id: string;
	status: string;
	message: string;
	image: string;
	slot: number;
	created_at: string;
}

export interface ServiceEvent {
	action: string;
	message: string;
	time: string;
}

export interface PortMapping {
	protocol: string;
	target_port: number;
	published_port: number;
	publish_mode: string;
}

export interface Deployment {
	id: string;
	app_id: string;
	status: string;
	commit_sha: string;
	image_digest: string;
	logs: string;
	started_at: string;
	finished_at: string | null;
}

export interface ManagedDatabase {
	id: string;
	project_id: string;
	name: string;
	db_type: string;
	version: string;
	status: string;
	created_at: string;
}

export interface SwarmNode {
	ID: string;
	Description: {
		Hostname: string;
		Platform: { Architecture: string; OS: string };
		Resources: { NanoCPUs: number; MemoryBytes: number };
	};
	Status: { State: string; Addr: string };
	Spec: { Role: string; Availability: string; Labels?: Record<string, string> };
}

export interface TemplateListItem {
	id: string;
	name: string;
	description: string;
	category: string;
	icon: string;
	image: string;
	version: string;
	tags: string[];
	links: Record<string, string>;
	ports: string[];
	env: Record<string, string>;
	volumes: string[];
	domain: string;
	replicas: number;
	is_stack: boolean;
	source: 'builtin' | 'custom';
	services?: StackServiceDef[];
	depends_on?: { type: string; version: string }[];
	nas_volumes?: { name: string; suggested_path: string; description: string }[];
}

export interface StackServiceDef {
	name: string;
	image: string;
	ports?: string[];
	env?: Record<string, string>;
	volumes?: string[];
}

export interface TemplateDetail extends TemplateListItem {
	compose_content?: string;
}

export interface DeployTemplateRequest {
	project_id?: string;
	domain?: string;
	env?: Record<string, string>;
	volumes?: string[];
	storage_host_id?: string;
	db_storage_mode?: string;
	db_node_id?: string;
}

export interface TemplateSource {
	id: string;
	org_id: string;
	name: string;
	url: string;
	type: string;
	last_synced_at: string | null;
	created_at: string;
}

export interface CustomTemplate {
	id: string;
	org_id: string;
	source_id: string;
	name: string;
	description: string;
	category: string;
	icon: string;
	image: string;
	version: string;
	ports: string;
	env: string;
	volumes: string;
	domain: string;
	replicas: number;
	is_stack: boolean;
	compose_content: string;
	created_at: string;
	updated_at: string;
}

export interface Secret {
	id: string;
	project_id: string;
	name: string;
	docker_secret_id: string;
	description: string;
	created_at: string;
	updated_at: string;
}

export interface AppSecret {
	app_id: string;
	secret_id: string;
	target: string;
	uid: string;
	gid: string;
	mode: number;
}

export interface Volume {
	id: string;
	project_id: string;
	name: string;
	driver: string;
	driver_opts: Record<string, string>;
	labels: Record<string, string>;
	mount_type: string;
	remote_host: string;
	remote_path: string;
	mount_options: string;
	scope: string;
	status: string;
	storage_host_id: string;
	local_path: string;
	ceph_pool: string;
	ceph_image: string;
	ceph_fs_name: string;
	created_at: string;
}

export interface AppVolume {
	app_id: string;
	volume_id: string;
	container_path: string;
	read_only: boolean;
}

export interface CreateVolumeRequest {
	name: string;
	mount_type?: string;
	remote_host?: string;
	remote_path?: string;
	mount_options?: string;
	username?: string;
	password?: string;
	storage_host_id?: string;
	local_path?: string;
	ceph_pool?: string;
	ceph_image?: string;
	ceph_fs_name?: string;
}

export interface BackupRun {
	id: string;
	config_id: string;
	status: string;
	size: number;
	target_path: string;
	started_at: string;
	finished_at: string | null;
}

export interface ServiceHealth {
	service_name: string;
	replicas: number;
	running: number;
	healthy: boolean;
	is_global?: boolean;
	nodes?: string[];
}

export interface NodeMetrics {
	node_id: string;
	hostname: string;
	cpu_percent: number;
	mem_used: number;
	mem_total: number;
	disk_used: number;
	disk_total: number;
	containers: number;
	services: number;
	timestamp: number;
}

export interface NotificationChannel {
	id: string;
	org_id: string;
	name: string;
	type: string;
	config: Record<string, string>;
	created_at: string;
}

export interface ProxyRoute {
	id: string;
	project_id: string;
	name: string;
	domain: string;
	target_service: string;
	target_port: number;
	protocol: string;
	upstream_port: number | null;
	ssl_mode: string;
	custom_cert_id: string;
	middleware_config: Record<string, unknown>;
	enabled: boolean;
	created_at: string;
}

export interface CreateProxyRouteRequest {
	name: string;
	domain: string;
	target_service: string;
	target_port?: number;
	ssl_mode?: string;
	custom_cert_id?: string;
	middleware_config?: Record<string, unknown>;
	enabled?: boolean;
}

export interface CustomCertificate {
	id: string;
	project_id: string;
	domain: string;
	cert_pem: string;
	is_wildcard: boolean;
	provider: string;
	expires_at: string | null;
	auto_renew: boolean;
	dns_provider_id: string;
	last_renewed_at: string | null;
	renewal_error: string;
	created_at: string;
}

export interface Stack {
	id: string;
	project_id: string;
	name: string;
	domain: string;
	compose_content: string;
	status: string;
	created_at: string;
	updated_at: string;
}

export interface UpdateStrategyRequest {
	strategy: string;
	parallelism: number;
	delay: string;
	failure_action: string;
	order: string;
}

export interface RegistryStatus {
	running: boolean;
	image_count?: number;
}

export interface RegistryImage {
	name: string;
	tags: string[];
}

export interface ConnectivityResult {
	port_80: boolean;
	port_443: boolean;
	message: string;
}

export interface AlertThreshold {
	id: string;
	org_id: string;
	metric: string;
	operator: string;
	value: number;
	cooldown_minutes: number;
	enabled: boolean;
	last_fired_at: string | null;
	created_at: string;
}

export interface BackupConfig {
	id: string;
	resource_id: string;
	schedule: string;
	s3_bucket: string;
	s3_prefix: string;
	backup_type: string;
	volume_id: string;
	created_at: string;
}

export interface StorageHost {
	id: string;
	name: string;
	node_id: string;
	address: string;
	type: string;
	default_export_path: string;
	default_mount_type: string;
	mount_options_default: string;
	capabilities: Record<string, boolean>;
	node_label: string;
	status: string;
	created_at: string;
	updated_at: string;
}

export interface CreateStorageHostRequest {
	name: string;
	node_id?: string;
	address: string;
	type?: string;
	default_export_path?: string;
	default_mount_type?: string;
	mount_options_default?: string;
	credentials?: string;
	capabilities?: Record<string, boolean>;
}

export interface StorageHostTestResult {
	host: string;
	address: string;
	type: string;
	ok: boolean;
	message: string;
}

export interface DNSProvider {
	id: string;
	org_id: string;
	name: string;
	type: string;
	is_default: boolean;
	created_at: string;
}

export interface DNSRecord {
	id: string;
	provider_id: string;
	app_id: string;
	domain: string;
	record_type: string;
	value: string;
	proxied: boolean;
	managed: boolean;
	external_id: string;
	created_at: string;
}

// Prometheus-backed metric types
export interface PrometheusClusterSummary {
	nodes: number;
	nodesUp: number;
	totalCores: number;
	totalRAM: number;
	totalDisk: number;
	usedDisk: number;
	avgCPU: number;
	containers: number;
}

export interface PrometheusNodeCurrent {
	hostname: string;
	nodeId: string;
	up: boolean;
	cpuPct: number;
	cores: number;
	memUsed: number;
	memTotal: number;
	diskUsed: number;
	diskTotal: number;
	uptimeSeconds: number;
	tempCelsius: number;
	containersRunning: number;
	loadAvg1: number;
}

export interface PrometheusTimeSeriesPoint {
	ts: number;
	value: number;
}

export interface PrometheusNodeHistory {
	hostname: string;
	cpu: PrometheusTimeSeriesPoint[];
	mem: PrometheusTimeSeriesPoint[];
}

export interface NodeMetricsReport {
	node_id: string;
	hostname: string;
	timestamp: number;
	cpu_cores: number;
	cpu_per_core: number[];
	cpu_total_pct: number;
	load_avg_1: number;
	load_avg_5: number;
	load_avg_15: number;
	cpu_temp_celsius: number;
	mem_total: number;
	mem_used: number;
	mem_available: number;
	mem_buffers: number;
	mem_cached: number;
	swap_total: number;
	swap_used: number;
	disks: DiskMetrics[];
	interfaces: NetInterface[];
	os: string;
	kernel: string;
	uptime_seconds: number;
	process_count: number;
	pending_updates: number;
	containers_running: number;
	containers_stopped: number;
	images_count: number;
	volumes_count: number;
	gpus?: GPUMetrics[];
}

export interface DiskMetrics {
	mount_point: string;
	device: string;
	fs_type: string;
	total: number;
	used: number;
	read_bytes: number;
	write_bytes: number;
	iops: number;
	smart_ok?: boolean;
}

export interface NetInterface {
	name: string;
	rx_bytes: number;
	tx_bytes: number;
	rx_packets: number;
	tx_packets: number;
	rx_errors: number;
	tx_errors: number;
	link_speed_mbps: number;
}

export interface GPUMetrics {
	index: number;
	name: string;
	util_pct: number;
	mem_used: number;
	mem_total: number;
	temp_celsius: number;
}

export interface AppEnvVar {
	id: string;
	app_id: string;
	key: string;
	value: string;
	is_secret: boolean;
	source: string;
	created_at: string;
	updated_at: string;
}

export interface ServiceLink {
	id: string;
	source_app_id: string;
	target_app_id: string;
	target_database_id: string;
	env_prefix: string;
	created_at: string;
}

export interface PreviewDeployment {
	id: string;
	app_id: string;
	branch: string;
	pr_number: number | null;
	domain: string;
	status: string;
	service_name: string;
	created_at: string;
}

export interface OrgRole {
	id: string;
	org_id: string;
	user_id: string;
	role: string;
	created_at: string;
}

export interface MaintenanceTask {
	id: string;
	org_id: string;
	type: string;
	schedule: string;
	enabled: boolean;
	last_run_at: string | null;
	last_status: string;
	config: Record<string, unknown>;
	created_at: string;
}

export interface MaintenanceRun {
	id: string;
	task_id: string;
	status: string;
	details: string;
	started_at: string;
	finished_at: string | null;
}

export interface AuditLogEntry {
	id: string;
	user_id: string;
	org_id: string;
	action: string;
	resource: string;
	resource_id: string;
	details: string;
	created_at: string;
}

export interface GitSource {
	id: string;
	provider: string;
	provider_name: string;
	created_at: string;
}

export interface GitRepository {
	full_name: string;
	name: string;
	clone_url: string;
	ssh_url: string;
	private: boolean;
	default_branch: string;
	description: string;
}

export interface GitBranch {
	name: string;
	protected: boolean;
	is_default: boolean;
}

export interface LogEntry {
	id: number;
	app_id: string;
	service_name: string;
	node_id: string;
	stream: string;
	message: string;
	level: string;
	timestamp: string;
}

export interface LogForwardConfig {
	id: string;
	org_id: string;
	name: string;
	type: string;
	enabled: boolean;
	created_at: string;
}

// Ceph Types

export interface CephCluster {
	id: string;
	name: string;
	fsid: string;
	status: string;
	bootstrap_node_id: string;
	mon_hosts: string[];
	public_network: string;
	cluster_network: string;
	replication_size: number;
	storage_host_id: string;
	created_at: string;
	updated_at: string;
}

export interface CephClusterWithHealth extends CephCluster {
	health?: CephHealthReport;
}

export interface CephHealthReport {
	fsid: string;
	node_id: string;
	timestamp: number;
	health: string;
	health_detail: string[];
	mon_count: number;
	mon_quorum: string[];
	osd_total: number;
	osd_up: number;
	osd_in: number;
	pg_count: number;
	pools: CephPoolStat[];
	total_bytes: number;
	used_bytes: number;
	avail_bytes: number;
}

export interface CephPoolStat {
	name: string;
	id: number;
	used_bytes: number;
	max_avail: number;
	objects: number;
}

export interface CephOSD {
	id: string;
	cluster_id: string;
	node_id: string;
	hostname: string;
	osd_id: number | null;
	device_path: string;
	device_size: number;
	device_type: string;
	status: string;
	created_at: string;
}

export interface CephPool {
	id: string;
	cluster_id: string;
	name: string;
	pool_id: number | null;
	pg_num: number;
	size: number;
	type: string;
	application: string;
	created_at: string;
}

export interface BlockDevice {
	name: string;
	path: string;
	size: number;
	type: string;
	mount_point?: string;
	fs_type?: string;
	model?: string;
	serial?: string;
	rotational: boolean;
	transport?: string;
	available: boolean;
}

export interface NodeDisks {
	node_id: string;
	block_devices: BlockDevice[];
}

export interface NodeAllDisks {
	node_id: string;
	hostname: string;
	block_devices: BlockDevice[];
}

export interface CreateCephClusterRequest {
	name: string;
	bootstrap_node_id: string;
	mon_nodes: { node_id: string; hostname: string; ip: string }[];
	osd_selections: { node_id: string; hostname: string; device_path: string; device_size?: number; device_type?: string }[];
	replication_size?: number;
	create_cephfs?: boolean;
	cephfs_name?: string;
	public_network?: string;
}

export interface UpdatesSummary {
	nodes_total: number;
	pending_updates: number;
	security_updates: number;
	reboot_required: number;
	service_updates: number;
}

export interface PendingPackage {
	name: string;
	current_version: string;
	new_version: string;
	is_security: boolean;
}

export interface NodeUpdateStatus {
	id: string;
	node_id: string;
	hostname: string;
	os_info: string;
	kernel_version: string;
	package_manager: string;
	pending_count: number;
	security_count: number;
	reboot_required: boolean;
	pending_packages: PendingPackage[];
	last_checked_at: string;
}

export interface ServiceUpdateStatus {
	id: string;
	app_id: string;
	stack_id: string;
	service_name: string;
	current_image: string;
	current_digest: string;
	latest_digest: string;
	latest_version: string;
	update_available: boolean;
	last_checked_at: string;
}

export interface UpdateEvent {
	id: string;
	event_type: string;
	target_type: string;
	target_id: string;
	target_name: string;
	previous_version: string;
	new_version: string;
	status: string;
	details: string;
	triggered_by: string;
	started_at: string;
	finished_at: string | null;
}

export interface UpdatePolicy {
	id: string;
	org_id: string;
	target_type: string;
	target_id: string;
	auto_update: boolean;
	auto_restart: boolean;
	maintenance_window_start: string;
	maintenance_window_end: string;
	maintenance_window_days: string;
	security_only: boolean;
	pre_update_backup: boolean;
	notify_on_update: boolean;
	created_at: string;
	updated_at: string;
}

// Docker Configs
export interface DockerConfig {
	id: string;
	project_id: string;
	org_id: string;
	name: string;
	docker_config_id: string;
	data: string;
	created_at: string;
	updated_at: string;
}

export interface AppConfig {
	id: string;
	app_id: string;
	config_id: string;
	target_path: string;
	uid: string;
	gid: string;
	mode: number;
}

export interface ScheduledJob {
	id: string;
	project_id: string;
	org_id: string;
	name: string;
	image: string;
	command: string;
	schedule: string;
	timezone: string;
	env: Record<string, string>;
	last_run_at: string | null;
	next_run_at: string | null;
	enabled: boolean;
	created_at: string;
}

export interface JobRun {
	id: string;
	job_id: string;
	status: string;
	started_at: string;
	finished_at: string | null;
	exit_code: number | null;
	logs: string;
	container_id: string;
}

export interface VulnerabilityScan {
	id: string;
	app_id: string;
	image: string;
	scan_status: string;
	started_at: string;
	completed_at: string | null;
	critical_count: number;
	high_count: number;
	medium_count: number;
	low_count: number;
}

export interface Vulnerability {
	id: string;
	scan_id: string;
	vuln_id: string;
	pkg_name: string;
	installed_version: string;
	fixed_version: string;
	severity: string;
	title: string;
	description: string;
}

export interface ResourceQuota {
	id: string;
	project_id: string;
	cpu_limit: number;
	memory_limit: number;
	storage_limit: number;
}

export interface NodePowerConfig {
	node_id: string;
	hostname: string;
	mac_address: string;
	bmc_address: string;
	wol_enabled: boolean;
}

export interface UPSDevice {
	id: string;
	name: string;
	nut_host: string;
	nut_port: number;
	ups_name: string;
	poll_interval_seconds: number;
	shutdown_threshold: number;
	created_at: string;
}

export interface UPSStatusSnapshot {
	id: string;
	device_id: string;
	status: string;
	battery_charge: number;
	battery_runtime: number;
	input_voltage: number;
	output_voltage: number;
	load_percent: number;
	temperature: number;
	timestamp: string;
}

export interface APIToken {
	id: string;
	name: string;
	token?: string;
	scopes: string;
	last_used_at: string | null;
	expires_at: string | null;
	created_at: string;
}

export interface WebhookEndpoint {
	id: string;
	name: string;
	url: string;
	secret: string;
	events: string;
	enabled: boolean;
	created_at: string;
}

export interface WebhookDelivery {
	id: string;
	webhook_id: string;
	event_type: string;
	payload: string;
	response_status: number;
	response_body: string;
	delivered_at: string;
}

export interface VPNServer {
	id: string;
	name: string;
	node_id: string;
	listen_port: number;
	address_range: string;
	dns: string;
	public_key: string;
	endpoint: string;
	enabled: boolean;
	peer_count: number;
	created_at: string;
}

export interface VPNPeer {
	id: string;
	server_id: string;
	name: string;
	public_key: string;
	allowed_ips: string;
	assigned_ip: string;
	last_handshake: string | null;
	transfer_rx: number;
	transfer_tx: number;
	created_at: string;
}

export interface OverlayNetwork {
	id: string;
	name: string;
	driver: string;
	scope: string;
	internal: boolean;
	attachable: boolean;
	encrypted: boolean;
	labels: Record<string, string>;
	created_at: string;
	containers: number;
}

export interface DashboardWidget {
	id: string;
	type: string;
	position: { x: number; y: number; w: number; h: number };
	config: Record<string, any>;
}

export interface ClusterInfo {
	id: string;
	name: string;
	api_endpoint: string;
	is_local: boolean;
	status: string;
	node_count: number;
	created_at: string;
}

export interface TemplateRatingEntry {
	id: string;
	template_name: string;
	user_id: string;
	rating: number;
	review_text: string;
	created_at: string;
}

export interface SearchResult {
	type: string;
	id: string;
	name: string;
	description: string;
	url: string;
}

export interface FileEntry {
	name: string;
	size: number;
	is_dir: boolean;
	mode: string;
	mod_time: string;
	owner: string;
}

export interface ContainerInfo {
	container_id: string;
	node_id: string;
	slot: number;
	image: string;
}

export interface SystemTask {
	id: string;
	name: string;
	description: string;
	category: string;
	interval_seconds: number;
	enabled: boolean;
	last_run_at: string | null;
	last_duration_ms: number;
	last_status: string;
	last_error: string;
	run_count: number;
	error_count: number;
	created_at: string;
	updated_at: string;
}

export interface BespokeAppClass {
	slug: string;
	name: string;
	description: string;
	template_name: string;
	category: string;
	recommended_ports: string[];
	recommended_env: Record<string, string>;
	notes: string[];
	template_available: boolean;
}
