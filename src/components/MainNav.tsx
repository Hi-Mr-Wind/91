import { useEffect, useRef, useState } from "react";
import { NavLink } from "react-router-dom";
import {
  Film,
  LogOut,
  Menu,
  Settings,
  Sparkles,
  Upload,
  X,
} from "lucide-react";
import { useAuth } from "@/admin/AuthContext";

const navItems = [
  { to: "/shorts", label: "短视频", icon: Sparkles },
  { to: "/list", label: "视频", icon: Film },
];

const uploadNavItem = { to: "/upload", label: "上传", icon: Upload };
const adminNavItem = { to: "/admin", label: "后台", icon: Settings };

export function MainNav() {
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLUListElement | null>(null);
  const toggleRef = useRef<HTMLButtonElement | null>(null);
  const { status, isAdmin, logout } = useAuth();

  const items = isAdmin ? [...navItems, uploadNavItem, adminNavItem] : navItems;

  const handleLogout = async () => {
    try {
      await logout();
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    if (!open) return;

    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (menuRef.current?.contains(target) || toggleRef.current?.contains(target)) {
        return;
      }
      setOpen(false);
    };

    document.addEventListener("pointerdown", handlePointerDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
    };
  }, [open]);

  return (
    <nav className={`main-nav ${open ? "is-open" : ""}`}>
      <div className="container main-nav__inner">
        <NavLink to="/" className="main-nav__logo">
          <span className="main-nav__logo-mark">
            <img src="/icon.png" alt="" className="main-nav__logo-img" />
          </span>
        </NavLink>

        <ul ref={menuRef} className="main-nav__list" role="menubar">
          {items.map(({ to, label, icon: Icon }) => (
            <li key={to} role="none">
              <NavLink
                to={to}
                role="menuitem"
                className={({ isActive }) =>
                  `main-nav__link ${isActive ? "is-active" : ""}`
                }
                onClick={() => {
                  setOpen(false);
                  if (to === "/shorts") {
                    const el = document.documentElement;
                    // eslint-disable-next-line @typescript-eslint/no-explicit-any
                    const fn = el.requestFullscreen?.bind(el) || (el as any).webkitRequestFullscreen?.bind(el);
                    if (fn) {
                      try {
                        const ret = fn();
                        if (ret && typeof ret.then === "function") {
                          ret.catch(() => {});
                        }
                      } catch {
                        // ignore
                      }
                    }
                  }
                }}
              >
                <Icon size={16} />
                {label}
              </NavLink>
            </li>
          ))}
          {status === "authed" && !isAdmin && (
            <li role="none">
              <button
                className="main-nav__link"
                role="menuitem"
                onClick={handleLogout}
              >
                <LogOut size={16} />
                退出
              </button>
            </li>
          )}
        </ul>

        <button
          ref={toggleRef}
          className="main-nav__toggle"
          aria-label={open ? "关闭菜单" : "打开菜单"}
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          {open ? <X size={22} /> : <Menu size={22} />}
        </button>
      </div>
    </nav>
  );
}
