// Updated CloudStorage Interface
interface CloudStorage {
	setItem(
		key: string,
		value: string,
		callback?: (error: Error | null, success: boolean) => void,
	): CloudStorage

	getItem(
		key: string,
		callback: (error: Error | null, value: string) => void,
	): void

	getItems(
		keys: string[],
		callback: (error: Error | null, values: string[]) => void,
	): void

	removeItem(
		key: string,
		callback?: (error: Error | null, success: boolean) => void,
	): CloudStorage

	removeItems(
		keys: string[],
		callback?: (error: Error | null, success: boolean) => void,
	): CloudStorage

	getKeys(callback: (error: Error | null, keys: string[]) => void): void
}

// Updated Telegram Interface
interface Telegram {
	WebView: WebView
	Utils: Utils
	WebApp: WebApp
}

interface Utils {
}

interface HapticFeedback {
	notificationOccurred(type: 'error' | 'success' | 'warning'): void

	impactOccurred(style: 'light' | 'medium' | 'heavy' | 'soft' | 'rigid'): void

	selectionChanged(): void
}

interface WebApp {
	initData: string
	initDataUnsafe: InitDataUnsafe
	version: string
	platform: string
	colorScheme: string
	themeParams: ThemeParams
	isExpanded: boolean
	viewportHeight: number
	viewportStableHeight: number
	safeAreaInset: SafeAreaInset
	contentSafeAreaInset: SafeAreaInset
	isClosingConfirmationEnabled: boolean
	isVerticalSwipesEnabled: boolean
	isFullscreen: boolean
	isOrientationLocked: boolean
	isActive: boolean
	headerColor: string
	backgroundColor: string
	bottomBarColor: string
	BackButton: BackButton
	MainButton: MainButton
	SecondaryButton: SecondaryButton
	SettingsButton: SettingsButton
	HapticFeedback: HapticFeedback
	CloudStorage: CloudStorage
	BiometricManager: BiometricManager
	Accelerometer: Accelerometer
	DeviceOrientation: DeviceOrientation
	Gyroscope: Gyroscope
	LocationManager: LocationManager

	openTelegramLink(url: string): void

	showAlert(message: string, callback?: () => void): void

	showConfirm(message: string, callback: (ok: boolean) => void): void

	expand(): void

	ready(): void

	close(): void

	onEvent(event: string, callback: () => void): void

	offEvent(event: string, callback: () => void): void

	openLink(url: string): void

	requestWriteAccess([callback]?: any): void

	sendData(data: any): void

	enableClosingConfirmation(): void

	disableClosingConfirmation(): void

	disableVerticalSwipes(): void

	enableVerticalSwipes(): void

	setBottomBarColor(color: string): void

	setHeaderColor(color: string): void
}

interface BackButton {
	isVisible: boolean

	onClick(callback: () => void): void

	offClick(callback: () => void): void

	show(): void

	hide(): void

	setParams(params: { text_color?: string }): void
}

interface MainButton {
	onClick: (callback: () => void) => MainButton
	text: string
	color: string
	offClick: (callback: () => void) => MainButton
	textColor: string
	isVisible: boolean
	isProgressVisible: boolean
	isActive: boolean
	hasShineEffect: boolean

	setParams(params: {
		text_color?: string
		color?: string
		text?: string
		is_active?: boolean
		is_visible?: boolean
	}): MainButton

	showProgress(leaveActive: boolean): void

	hideProgress(): void

	disable(): void

	setText(nextText: string): void

	show(): void

	enable(): void
}

interface SecondaryButton {
	type: string
	text: string
	color: string
	textColor: string
	isVisible: boolean
	isProgressVisible: boolean
	isActive: boolean
	hasShineEffect: boolean
	position: string
}

interface SettingsButton {
	isVisible: boolean
}

interface BiometricManager {
	isInited: boolean
	isBiometricAvailable: boolean
	biometricType: string
	isAccessRequested: boolean
	isAccessGranted: boolean
	isBiometricTokenSaved: boolean
	deviceId: string
}

interface Accelerometer {
	isStarted: boolean
	x: number | null
	y: number | null
	z: number | null
}

interface DeviceOrientation {
	isStarted: boolean
	absolute: boolean
	alpha: number | null
	beta: number | null
	gamma: number | null
}

interface Gyroscope {
	isStarted: boolean
	x: number | null
	y: number | null
	z: number | null
}

interface LocationManager {
	isInited: boolean
	isLocationAvailable: boolean
	isAccessRequested: boolean
	isAccessGranted: boolean
}

interface SafeAreaInset {
	top: number
	bottom: number
	left: number
	right: number
}

interface InitDataUnsafe {
	query_id: string
	user: User
	auth_date: string
	signature: string
	hash: string
	start_param?: string
}

interface User {
	id: number
	first_name: string
	last_name: string
	username: string
	language_code: string
	is_bot: boolean
	is_premium: boolean
	added_to_attachment_menu?: boolean
	allows_write_to_pm: boolean
	photo_url: string
}

interface ThemeParams {
	bg_color: string
	text_color: string
	hint_color: string
	link_color: string
	button_color: string
	button_text_color: string
	secondary_bg_color: string
	header_bg_color: string
	accent_text_color: string
	section_bg_color: string
	section_header_text_color: string
	subtitle_text_color: string
	destructive_text_color: string
}

interface WebView {
	initParams: InitParams
	isIframe: boolean
}

interface InitParams {
	tgWebAppData: string
	tgWebAppVersion: string
	tgWebAppThemeParams: string
}
