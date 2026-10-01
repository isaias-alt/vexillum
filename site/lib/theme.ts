// Theme handling without a library: the choice lives in localStorage and the
// `data-theme` attribute on <html> (not a class: React owns <html>'s className
// and rewrites it when the [lang] layout remounts). Dark is the brand default;
// only an explicit "light" switches it off.
export const THEME_KEY = "vexillum-theme";
export const THEME_EVENT = "vexillum-theme-change";

export type Theme = "dark" | "light";

// Runs blocking in <head> (see components/ThemeScript.tsx), before first paint,
// so there is no flash. Storage can throw (private mode, blocked data): then
// the default applies.
export const THEME_INIT_SCRIPT = `(function(){var t=null;try{t=localStorage.getItem(${JSON.stringify(THEME_KEY)})}catch(e){}document.documentElement.setAttribute("data-theme",t==="light"?"light":"dark")})()`;
