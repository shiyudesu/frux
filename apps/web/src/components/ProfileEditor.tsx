import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import type { Gender, ProfileSettings } from "../types";
import { useDialogFocus } from "../hooks/useDialogFocus";
import { image } from "../constants";
import { Icon } from "./Icon";

export interface ProfileEditorValue {
  nickname: string;
  avatarURL: string;
  bio: string;
  gender: Gender;
  settings: ProfileSettings;
}

interface ProfileEditorProps {
  value: ProfileEditorValue;
  busy: boolean;
  message?: string;
  onClose: () => void;
  onSave: (value: ProfileEditorValue, avatarFile: File | null) => Promise<void>;
}

export function ProfileEditor({ value, busy, message, onClose, onSave }: ProfileEditorProps) {
  const [form, setForm] = useState(value);
  const [avatarFile, setAvatarFile] = useState<File | null>(null);
  const [avatarPreview, setAvatarPreview] = useState("");
  const closeRef = useDialogFocus<HTMLButtonElement>(true, onClose);

  useEffect(() => {
    if (!avatarFile) {
      setAvatarPreview("");
      return;
    }
    const objectURL = URL.createObjectURL(avatarFile);
    setAvatarPreview(objectURL);
    return () => URL.revokeObjectURL(objectURL);
  }, [avatarFile]);

  function setLikedVisibility(publiclyVisible: boolean) {
    setForm((current) => ({
      ...current,
      settings: {
        ...current.settings,
        liked_visibility: publiclyVisible ? "public" : "private",
        favorite_visibility: "private"
      }
    }));
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    await onSave(form, avatarFile);
  }

  return (
    <div className="modal-backdrop profile-editor-backdrop" role="presentation" onMouseDown={onClose}>
      <form
        aria-labelledby="profile-editor-title"
        aria-modal="true"
        className="profile-modal profile-editor"
        role="dialog"
        onMouseDown={(event) => event.stopPropagation()}
        onSubmit={submit}
      >
        <header>
          <h2 id="profile-editor-title">编辑资料</h2>
          <button ref={closeRef} className="icon-button small" type="button" onClick={onClose} aria-label="关闭">
            <Icon name="close" size={19} />
          </button>
        </header>
        <label className="profile-avatar-editor">
          <span className="profile-avatar-editor-image">
            <img src={avatarPreview || form.avatarURL || image.currentUser} alt="头像预览" />
            <span><Icon name="camera" size={22} /></span>
          </span>
          <strong>点击修改头像</strong>
          <input type="file" accept="image/*" onChange={(event) => setAvatarFile(event.target.files?.[0] || null)} />
        </label>
        <label className="profile-editor-field">
          <span>昵称 <small>{form.nickname.length}/20</small></span>
          <input
            maxLength={20}
            required
            value={form.nickname}
            onChange={(event) => setForm({ ...form, nickname: event.target.value })}
          />
        </label>
        <label className="profile-editor-field">
          <span>简介 <small>{form.bio.length}/200</small></span>
          <textarea
            maxLength={200}
            rows={4}
            placeholder="介绍一下你自己"
            value={form.bio}
            onChange={(event) => setForm({ ...form, bio: event.target.value })}
          />
        </label>
        <fieldset className="profile-editor-options">
          <legend>性别</legend>
          {[
            { value: 0, label: "未设置" },
            { value: 1, label: "男" },
            { value: 2, label: "女" },
            { value: 3, label: "其他" }
          ].map((item) => (
            <label key={item.value}>
              <input
                checked={form.gender === item.value}
                name="gender"
                type="radio"
                onChange={() => setForm({ ...form, gender: item.value as Gender })}
              />
              {item.label}
            </label>
          ))}
        </fieldset>
        <fieldset className="profile-editor-options privacy-options">
          <legend>主页隐私</legend>
          <label>
            <span>公开喜欢列表</span>
            <input
              checked={form.settings.liked_visibility === "public"}
              type="checkbox"
              onChange={(event) => setLikedVisibility(event.target.checked)}
            />
          </label>
          <label>
            <span>收藏列表</span>
            <strong>仅自己可见</strong>
          </label>
        </fieldset>
        {message && <p className="form-message">{message}</p>}
        <footer className="profile-editor-actions">
          <button type="button" onClick={onClose}>取消</button>
          <button className="primary-button" disabled={busy} type="submit">
            {busy ? "保存中" : "保存"}
          </button>
        </footer>
      </form>
    </div>
  );
}
