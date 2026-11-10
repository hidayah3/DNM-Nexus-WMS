import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import '../css/ActionMenu.css';

const MENU_WIDTH = 232;
const ITEM_HEIGHT = 46;

// items: [{ key, label, onSelect, disabled, reason, tone }]
// Unavailable items stay visible, but are greyed out, show why, and cannot be clicked.
export default function ActionMenu({ label = 'Actions', ariaLabel, items }) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState({ top: 0, left: 0 });
  const buttonRef = useRef(null);
  const menuRef = useRef(null);

  const toggle = () => {
    if (open) {
      setOpen(false);
      return;
    }
    // The menu is drawn on document.body so the table's scroll area cannot clip it.
    const rect = buttonRef.current.getBoundingClientRect();
    const height = items.length * ITEM_HEIGHT + 12;
    const openUp = window.innerHeight - rect.bottom < height + 8 && rect.top > height + 8;
    setPos({
      top: openUp ? rect.top - height - 4 : rect.bottom + 4,
      left: Math.max(8, Math.min(rect.right - MENU_WIDTH, window.innerWidth - MENU_WIDTH - 8))
    });
    setOpen(true);
  };

  useEffect(() => {
    if (!open) return undefined;
    const close = () => setOpen(false);
    const onDown = e => {
      if (menuRef.current?.contains(e.target) || buttonRef.current?.contains(e.target)) return;
      close();
    };
    const onKey = e => {
      if (e.key === 'Escape') {
        close();
        buttonRef.current?.focus();
      }
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    window.addEventListener('scroll', close, true);
    window.addEventListener('resize', close);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
      window.removeEventListener('scroll', close, true);
      window.removeEventListener('resize', close);
    };
  }, [open]);

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        className="am-trigger"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={ariaLabel}
        onClick={toggle}
      >
        {label}
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
          <path d="M2 3.5 5 6.5 8 3.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>

      {open && createPortal(
        <div ref={menuRef} className="am-menu" role="menu" style={{ top: pos.top, left: pos.left, width: MENU_WIDTH }}>
          {items.map(it => (
            <button
              key={it.key}
              type="button"
              role="menuitem"
              aria-disabled={it.disabled ? 'true' : undefined}
              className={`am-item ${it.tone || ''} ${it.disabled ? 'is-disabled' : ''}`}
              onClick={() => {
                if (it.disabled) return;
                setOpen(false);
                it.onSelect();
              }}
            >
              {it.label}
              {it.disabled && it.reason && <small className="am-reason">{it.reason}</small>}
            </button>
          ))}
        </div>,
        document.body
      )}
    </>
  );
}