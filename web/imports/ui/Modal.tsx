import React, { useEffect, useRef } from "react";

export function Modal({title,onClose,children,className}:{title:string;onClose:()=>void;children:React.ReactNode;className?:string}) {
  const dialog=useRef<HTMLDialogElement>(null);
  useEffect(()=>{const node=dialog.current;node?.showModal();return()=>node?.close();},[]);
  return <dialog ref={dialog} className={className ? `modal ${className}` : "modal"} aria-label={title} onCancel={e=>{e.preventDefault();onClose();}} onClick={e=>{if(e.target===e.currentTarget)onClose();}}>
    <div className="modal-heading"><h2>{title}</h2><button aria-label="닫기" onClick={onClose}>×</button></div>
    {children}
  </dialog>;
}
