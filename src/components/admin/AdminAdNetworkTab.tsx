import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { PreMadeWidget } from '../../types';
import { AdminAdFraudReport } from './AdminAdFraudReport';
import { Code2, Plus, Edit2, X } from 'lucide-react';

export const AdminAdNetworkTab: React.FC = () => {
  const { preMadeWidgets, createPreMadeWidget, updatePreMadeWidget } = useStore();
  const { success } = useToast();
  const [showAddModal, setShowAddModal] = useState(false);
  const [editingWidget, setEditingWidget] = useState<PreMadeWidget | null>(null);
  const [newTitle, setNewTitle] = useState('');
  const [newSnippet, setNewSnippet] = useState('');

  return (
    <div className="space-y-3.5 pb-6">
      <AdminAdFraudReport />

      {/* 3. Pre-made Widgets & HTML Banners for Sellers (Section 4.2 & 4.6) */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-indigo-600">
            <Code2 className="w-4 h-4"  strokeWidth={1.5}/>
            <h4 className="text-xs font-bold text-slate-800">ویجت‌ها و بنرهای آماده پلتفرم برای ویترین‌ها</h4>
          </div>
          <button
            type="button"
            onClick={() => setShowAddModal(true)}
            className="flex items-center gap-1 bg-indigo-50 hover:bg-indigo-100 text-indigo-700 font-bold text-[10px] px-2.5 py-1.5 rounded-xl transition-colors"
          >
            <Plus className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            <span>تعریف بنر آماده جدید</span>
          </button>
        </div>

        <div className="space-y-2">
          {preMadeWidgets.map((wgt) => (
            <div key={wgt.id} className="p-3 bg-slate-50 border border-slate-200 rounded-xl space-y-1.5 text-xs">
              <div className="flex items-center justify-between">
                <span className="font-bold text-slate-900">{wgt.title}</span>
                <div className="flex items-center gap-1.5">
                  <span className="text-[9px] font-mono bg-white px-2 py-0.5 rounded border border-slate-200">{wgt.type}</span>
                  <button
                    onClick={() => setEditingWidget({ ...wgt })}
                    className="p-1 rounded-md text-slate-400 hover:text-blue-600 hover:bg-white"
                    title="ویرایش بنر و مشخصات"
                  >
                    <Edit2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                  </button>
                </div>
              </div>
              <p className="text-[10px] text-slate-500">{wgt.description}</p>
              {wgt.htmlCodeSnippet && (
                <div className="bg-slate-900 text-emerald-400 p-2 rounded-lg font-mono text-[9px] overflow-x-auto no-scrollbar" dir="ltr">
                  {wgt.htmlCodeSnippet}
                </div>
              )}
            </div>
          ))}
        </div>
      </div>

      {showAddModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <div className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl border border-slate-100 space-y-3 text-xs">
            <h4 className="font-bold text-slate-900">تعریف بنر تبلیغاتی جدید برای دراپ‌شیپرها</h4>
            <div>
              <label className="text-[10px] text-slate-600 block mb-1">عنوان بنر</label>
              <input
                type="text"
                placeholder="مثال: بنر تبلیغاتی ۷۲۸ در ۹۰ کمپین فصلی"
                value={newTitle}
                onChange={(e) => setNewTitle(e.target.value)}
                className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
              />
            </div>
            <div>
              <label className="text-[10px] text-slate-600 block mb-1">کد HTML بنر (شامل ردیابی {'{{STORE_SLUG}}'})</label>
              <textarea
                rows={3}
                placeholder="<a href='https://sellershow.ir/?ad_token={{STORE_SLUG}}'><img .../></a>"
                value={newSnippet}
                onChange={(e) => setNewSnippet(e.target.value)}
                className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-mono focus:outline-hidden"
                dir="ltr"
              />
            </div>
            <div className="flex gap-2 pt-1">
              <button
                type="button"
                onClick={() => {
                  if (!newTitle.trim()) return;
                  createPreMadeWidget({
                    title: newTitle.trim(),
                    type: 'html_banner',
                    description: 'بنر آماده ساخته‌شده توسط مدیریت پلتفرم جهت بازاریابی اختصاصی سلرها',
                    htmlCodeSnippet: newSnippet.trim() || `<a href="https://sellershow.ir/?ad_token={{STORE_SLUG}}"><img src="https://web-cdn.snapp.ir/fintech-marketing/Campaign/Del/140507/Banners/shop-ino_Slider_Banners.png" alt="${newTitle}" style="max-width:100%;border-radius:12px;" /></a>`,
                  });
                  success('بنر جدید با موفقیت اضافه شد و در تب بازاریابی سلرها قرار گرفت.');
                  setShowAddModal(false);
                  setNewTitle('');
                  setNewSnippet('');
                }}
                className="flex-1 py-2 bg-indigo-600 hover:bg-indigo-700 text-white font-bold text-xs rounded-xl"
              >
                ثبت و انتشار در شبکه
              </button>
              <button
                type="button"
                onClick={() => setShowAddModal(false)}
                className="px-3 py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl"
              >
                انصراف
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Requirement 22: Edit Pre-made Widget / Banner */}
      {editingWidget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <div className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl border border-slate-100 space-y-3 text-xs">
            <div className="flex items-center justify-between border-b pb-2">
              <h4 className="font-bold text-slate-900">ویرایش بنر تبلیغاتی</h4>
              <button onClick={() => setEditingWidget(null)}><X className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/></button>
            </div>
            <div>
              <label className="text-[10px] text-slate-600 block mb-1">عنوان بنر</label>
              <input
                type="text"
                value={editingWidget.title}
                onChange={(e) => setEditingWidget({ ...editingWidget, title: e.target.value })}
                className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200"
              />
            </div>
            <div>
              <label className="text-[10px] text-slate-600 block mb-1">لینک تصویر پیش‌نمایش</label>
              <input
                type="text"
                value={editingWidget.previewImageUrl || ''}
                onChange={(e) => setEditingWidget({ ...editingWidget, previewImageUrl: e.target.value })}
                className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-mono text-[10px]"
                dir="ltr"
              />
            </div>
            <div>
              <label className="text-[10px] text-slate-600 block mb-1">کد HTML بنر</label>
              <textarea
                rows={3}
                value={editingWidget.htmlCodeSnippet || ''}
                onChange={(e) => setEditingWidget({ ...editingWidget, htmlCodeSnippet: e.target.value })}
                className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-mono"
                dir="ltr"
              />
            </div>
            <div className="flex gap-2 pt-1">
              <button
                type="button"
                onClick={() => {
                  updatePreMadeWidget(editingWidget.id, {
                    title: editingWidget.title,
                    previewImageUrl: editingWidget.previewImageUrl,
                    htmlCodeSnippet: editingWidget.htmlCodeSnippet,
                  });
                  success('تغییرات بنر ذخیره شد.');
                  setEditingWidget(null);
                }}
                className="flex-1 py-2 bg-indigo-600 hover:bg-indigo-700 text-white font-bold text-xs rounded-xl"
              >
                ذخیره تغییرات
              </button>
              <button
                type="button"
                onClick={() => setEditingWidget(null)}
                className="px-3 py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl"
              >
                انصراف
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
