import { useState } from "react";
import type { FormEvent } from "react";
import { changeMyPassword } from "../api/account";
import { UserFacingError, apiErrorMessage } from "../api/client";
import { useDialogFocus } from "../hooks/useDialogFocus";
import { PASSWORD_RULE_MESSAGE, validateSelectedPassword } from "../passwordPolicy";
import { useSession } from "../session";
import { Icon } from "./Icon";

interface PasswordChangeDialogProps {
  onClose: () => void;
}

interface PasswordChangeForm {
  currentPassword: string;
  nextPassword: string;
  confirmPassword: string;
}

const EMPTY_FORM: PasswordChangeForm = {
  currentPassword: "",
  nextPassword: "",
  confirmPassword: ""
};

export function PasswordChangeDialog({ onClose }: PasswordChangeDialogProps) {
  const session = useSession();
  const [form, setForm] = useState<PasswordChangeForm>(EMPTY_FORM);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [success, setSuccess] = useState(false);
  const closeRef = useDialogFocus<HTMLButtonElement>(true, onClose);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (!session.token) return;
    setBusy(true);
    setMessage("");
    setSuccess(false);
    try {
      const currentPassword = form.currentPassword;
      if (!currentPassword) throw new UserFacingError("请输入当前密码");
      const passwordMessage = validateSelectedPassword(form.nextPassword);
      if (passwordMessage) throw new UserFacingError(passwordMessage);
      if (form.nextPassword !== form.confirmPassword) {
        throw new UserFacingError("两次输入的新密码不一致");
      }
      await session.runCredentialMutation(async () => {
        const tokenResponse = await changeMyPassword({
          current_password: currentPassword,
          new_password: form.nextPassword
        }, session.token);
        session.replaceAccessToken(
          tokenResponse.access_token,
          tokenResponse.expires_in_seconds
        );
      }, true);
      setForm(EMPTY_FORM);
      setSuccess(true);
      setMessage("密码已更新，当前设备将继续保持登录");
    } catch (error) {
      setSuccess(false);
      setMessage(apiErrorMessage(error, "密码修改失败，请稍后重试"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-backdrop profile-editor-backdrop" role="presentation" onMouseDown={onClose}>
      <form
        aria-describedby="password-change-description"
        aria-labelledby="password-change-title"
        aria-modal="true"
        className="profile-modal profile-security-dialog"
        role="dialog"
        onMouseDown={(event) => event.stopPropagation()}
        onSubmit={handleSubmit}
      >
        <header>
          <div>
            <h2 id="password-change-title">修改密码</h2>
            <p id="password-change-description" className="profile-security-note">
              {PASSWORD_RULE_MESSAGE}
            </p>
          </div>
          <button ref={closeRef} className="icon-button small" type="button" onClick={onClose} aria-label="关闭">
            <Icon name="close" size={19} />
          </button>
        </header>
        <label className="profile-editor-field">
          <span>当前密码</span>
          <input
            autoComplete="current-password"
            type="password"
            value={form.currentPassword}
            onChange={(event) => setForm({ ...form, currentPassword: event.target.value })}
          />
        </label>
        <label className="profile-editor-field">
          <span>新密码</span>
          <input
            autoComplete="new-password"
            type="password"
            value={form.nextPassword}
            onChange={(event) => setForm({ ...form, nextPassword: event.target.value })}
          />
        </label>
        <label className="profile-editor-field">
          <span>确认新密码</span>
          <input
            autoComplete="new-password"
            type="password"
            value={form.confirmPassword}
            onChange={(event) => setForm({ ...form, confirmPassword: event.target.value })}
          />
        </label>
        {message && <p className={`form-message${success ? " success" : ""}`}>{message}</p>}
        <footer className="profile-editor-actions profile-security-actions">
          <button type="button" onClick={onClose}>取消</button>
          <button className="primary-button" disabled={busy} type="submit">
            {busy ? "保存中" : success ? "已更新" : "确认修改"}
          </button>
        </footer>
      </form>
    </div>
  );
}
