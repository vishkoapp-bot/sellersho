import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { TicketConversationModal } from '../common/TicketConversationModal';
import { Headphones, MessageSquare, Send, CheckCircle2, MessageCircle } from 'lucide-react';

export const SupportTicketsTab: React.FC = () => {
  const [activeFilter, setActiveFilter] = useState<'all' | 'customer' | 'seller' | 'supplier'>('all');
  const [replyText, setReplyText] = useState<{ [key: string]: string }>({});
  const [answeredMap, setAnsweredMap] = useState<{ [key: string]: boolean }>({});
  const [conversationTicket, setConversationTicket] = useState<any | null>(null);

  const tickets = [
    {
      id: 'TCK-9041',
      role: 'مشتری خریدار',
      senderName: 'محمد رضایی',
      subject: 'استعلام زمان ارسال بسته پستی ORD-702774',
      store: 'فروشگاه بی‌بی استایل',
      department: 'لجستیک و انبار',
      time: 'امروز - ۱۰:۴۵',
      message: 'سلام، می‌خواستم بدونم مرسوله پستی کی به اداره پست تحویل داده میشه؟',
      status: 'pending',
    },
    {
      id: 'TCK-9040',
      role: 'دراپ‌شیپر',
      senderName: 'آرش صادقی (تک‌هاب)',
      subject: 'درخواست راهنمایی در تنظیم CNAME دامنه اختصاصی',
      store: 'تک‌هاب پلاس',
      department: 'پشتیبانی فنی',
      time: 'امروز - ۰۹:۳۰',
      message: 'رکوردهای CNAME روی کلودفلر تنظیم شد، اما هنوز گواهی SSL در حالت انتظار قرار دارد.',
      status: 'pending',
    },
  ];

  const handleSendReply = (ticketId: string) => {
    setAnsweredMap(prev => ({ ...prev, [ticketId]: true }));
  };

  return (
    <div className="space-y-3.5 pb-6">
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-blue-600">
            <Headphones className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">میز پشتیبانی و تیکتینگ یکپارچه پلتفرم</h3>
          </div>
          <span className="text-[10px] bg-blue-50 text-blue-700 font-bold px-2 py-0.5 rounded-full">
            پاسخگویی سریع
          </span>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          مشتری در محیط ویترین برنددار تیکت می‌زند، اما پشت صحنه بر اساس نوع درخواست به دراپ‌شیپر، انبار یا واحد مالی ارجاع می‌شود (بخش ۲۰ پروپوزال).
        </p>
      </div>

      <div className="space-y-3">
        {tickets.map((t) => {
          const isAnswered = answeredMap[t.id];
          return (
            <div key={t.id} className="p-3.5 bg-white rounded-2xl border border-slate-200/90 shadow-2xs space-y-2.5 text-xs">
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <div>
                  <span className="font-bold text-slate-800 block">{t.subject}</span>
                  <span className="text-[10px] text-slate-400 mt-0.5 block font-mono">
                    {t.id} • {t.role}: {t.senderName} ({t.store})
                  </span>
                </div>
                <button
                  onClick={() => setConversationTicket(t)}
                  className="text-[10px] text-blue-600 font-bold bg-blue-50 px-2 py-0.5 rounded-lg hover:bg-blue-100 flex items-center gap-1"
                >
                  <MessageCircle className="w-3 h-3"  strokeWidth={1.5}/>
                  <span>مشاهده تاریخچه و گفت‌وگو</span>
                </button>
              </div>

              <div className="bg-slate-50 p-2.5 rounded-xl text-[11px] text-slate-700 leading-relaxed">
                {t.message}
              </div>

              {isAnswered ? (
                <div className="p-2 bg-emerald-50 text-emerald-700 text-[10px] font-bold rounded-xl flex items-center justify-center gap-1">
                  <CheckCircle2 className="w-4 h-4 text-emerald-600"  strokeWidth={1.5}/>
                  <span>پاسخ رسمی پشتیبانی برای کاربر ثبت شد.</span>
                </div>
              ) : (
                <div className="space-y-2 pt-1 border-t border-slate-100">
                  <input
                    type="text"
                    placeholder="درج پاسخ رسمی کارشناس پشتیبانی..."
                    value={replyText[t.id] || ''}
                    onChange={(e) => setReplyText({ ...replyText, [t.id]: e.target.value })}
                    className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
                  />
                  <button
                    onClick={() => handleSendReply(t.id)}
                    className="w-full py-1.5 bg-[#007dfa] hover:bg-blue-600 text-white font-bold text-xs rounded-xl shadow-xs transition-colors flex items-center justify-center gap-1"
                  >
                    <Send className="w-3 h-3"  strokeWidth={1.5}/>
                    <span>ارسال پاسخ و بستن پرونده</span>
                  </button>
                </div>
              )}
            </div>
          );
        })}
      </div>

      <TicketConversationModal
        isOpen={!!conversationTicket}
        ticket={conversationTicket}
        onClose={() => setConversationTicket(null)}
        isAdminReply={true}
      />
    </div>
  );
};
