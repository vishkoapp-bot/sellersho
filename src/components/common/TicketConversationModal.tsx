import React, { useState } from 'react';
import { useToast } from '../../context/ToastContext';
import { X, Headphones, Send, User, CheckCircle2 } from 'lucide-react';
import { motion } from 'motion/react';

interface Props {
  ticket: any | null;
  isOpen: boolean;
  onClose: () => void;
  isAdminReply?: boolean;
}

export const TicketConversationModal: React.FC<Props> = ({ ticket, isOpen, onClose, isAdminReply = false }) => {
  const { success } = useToast();
  const [messages, setMessages] = useState<Array<{ sender: string; text: string; time: string; isSelf: boolean }>>([
    {
      sender: 'کاربر / درخواست‌دهنده',
      text: ticket?.message || 'شرح اولیه تیکت ثبت شده در سیستم...',
      time: 'امروز ۱۰:۳۰',
      isSelf: false,
    },
    {
      sender: 'پشتیبانی مرکزی سلرشو',
      text: ticket?.response || 'درخواست شما بررسی شد و اقدامات لازم در دست اقدام است.',
      time: 'امروز ۱۱:۰۰',
      isSelf: true,
    },
  ]);
  const [newMsg, setNewMsg] = useState('');

  if (!isOpen || !ticket) return null;

  const handleSend = (e: React.FormEvent) => {
    e.preventDefault();
    if (!newMsg.trim()) return;

    setMessages(prev => [
      ...prev,
      {
        sender: isAdminReply ? 'کارشناس پشتیبانی' : 'من (سلر / مشتری)',
        text: newMsg.trim(),
        time: 'هم‌اکنون',
        isSelf: true,
      },
    ]);
    setNewMsg('');
    success('پاسخ شما در رشته گفت‌وگوی تیکت ثبت شد.');
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 12 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-2xl max-w-[420px] w-full p-4 shadow-2xl border border-slate-100 flex flex-col max-h-[85vh] overflow-hidden space-y-3"
      >
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-xl bg-brand-soft text-brand-deep flex items-center justify-center">
              <Headphones className="w-4 h-4"  strokeWidth={1.5}/>
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900 truncate max-w-[200px]">{ticket.subject || ticket.title}</h3>
              <span className="text-[10px] text-slate-400 font-mono block">شناسه: {ticket.id}</span>
            </div>
          </div>
          <button onClick={onClose} className="p-1 rounded-lg text-slate-400 hover:bg-slate-100">
            <X className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>

        {/* Thread */}
        <div className="overflow-y-auto no-scrollbar space-y-2.5 flex-1 pr-1">
          {messages.map((m, idx) => (
            <div
              key={idx}
              className={`p-2.5 rounded-2xl text-xs space-y-1 ${
                m.isSelf
                  ? 'bg-brand-soft text-brand-deep mr-4 border border-brand-border'
                  : 'bg-slate-50 text-slate-800 ml-4 border border-slate-200'
              }`}
            >
              <div className="flex justify-between items-center text-[10px] text-slate-400">
                <span className="font-bold">{m.sender}</span>
                <span>{m.time}</span>
              </div>
              <p className="leading-relaxed text-[11px]">{m.text}</p>
            </div>
          ))}
        </div>

        {/* Reply form */}
        <form onSubmit={handleSend} className="pt-2 border-t border-slate-100 flex gap-1.5">
          <input
            type="text"
            placeholder="پاسخ به تیکت..."
            value={newMsg}
            onChange={(e) => setNewMsg(e.target.value)}
            className="flex-1 text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
          />
          <button
            type="submit"
            className="p-2 bg-brand hover:bg-brand-deep text-white rounded-xl shadow-xs transition-colors shrink-0"
          >
            <Send className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </form>
      </motion.div>
    </div>
  );
};
