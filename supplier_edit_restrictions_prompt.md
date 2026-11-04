# پرامپت: محدود کردن ویرایش محصولات Supplier + اضافه کردن ویرایش موجودی

## 🎯 هدف

تامین‌کننده (Supplier) نباید بتواند ویژگی‌ها، تصویر یا اطلاعات اصلی محصول را تغییر دهد، زیرا این تغییرات باعث نامشخص شدن سفارشات قبلی می‌شود.

**فیلدهای قابل ویرایش:**
- ✅ قیمت عمده (supplierBaseCost / C)
- ✅ موجودی (stock)

**فیلدهای غیرقابل ویرایش:**
- ❌ عنوان محصول (title)
- ❌ توضیحات (description)
- ❌ مشخصات فنی (specs) - رنگ، سایز، جنس، ...
- ❌ تصویر (imageUrl / images)
- ❌ دسته‌بندی (category)
- ❌ نیچ (niches)
- ❌ بارکد (barcode)
- ❌ گارانتی (warranty)
- ❌ انبار (warehouse)
- ❌ زمان آماده‌سازی (preparationDays)
- ❌ اصالت کالا (isOriginal)

---

## 📋 تغییرات مورد نیاز

### 1️⃣ Frontend: اضافه کردن دکمه ویرایش موجودی

**فایل:** `src/components/supplier/SupplierProductsTab.tsx`

**مکان:** خط 596 - کنار دکمه "ویرایش قیمت عمده C"

**تغییرات:**

#### الف) اضافه کردن state جدید:

```typescript
// در بالای کامپوننت، کنار state های موجود:
const [editingProduct, setEditingProduct] = useState<Product | null>(null);
const [editedCostC, setEditedCostC] = useState('');

// state های جدید برای ویرایش موجودی:
const [editingStockProduct, setEditingStockProduct] = useState<Product | null>(null);
const [editedStock, setEditedStock] = useState('');
```

#### ب) اضافه کردن UI ویرایش موجودی:

در بخش نمایش لیست محصولات (خط 559-599)، **بعد از** بخش ویرایش قیمت، این کد را اضافه کن:

```typescript
{/* بخش ویرایش موجودی */}
{editingStockProduct?.id === product.id ? (
  <div className="pt-2 border-t border-slate-100 flex items-center gap-2">
    <input
      type="number"
      value={editedStock}
      onChange={(e) => setEditedStock(e.target.value)}
      placeholder="موجودی جدید"
      className="flex-1 text-xs p-1.5 rounded-lg bg-slate-50 border border-slate-200 font-bold font-mono"
    />
    <button
      type="button"
      onClick={() => {
        const newStock = Number(editedStock);
        if (isNaN(newStock) || newStock < 0) {
          error('موجودی باید عدد مثبت باشد');
          return;
        }
        
        // فراخوانی API برای به‌روزرسانی موجودی
        fetch(`/api/v1/supplier/products/${product.id}/stock`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            new_stock: newStock,
            reason: 'تغییر موجودی توسط تامین‌کننده'
          })
        })
          .then(res => res.json())
          .then(data => {
            if (data.success) {
              success(`موجودی محصول «${product.title}» به ${toPersianDigits(newStock)} عدد تغییر یافت.`);
              // به‌روزرسانی local state
              setApprovedProducts(prev => 
                prev.map(p => p.id === product.id ? {...p, stock: newStock} : p)
              );
              setEditingStockProduct(null);
              setEditedStock('');
            } else {
              error(data.error || 'خطا در تغییر موجودی');
            }
          })
          .catch(() => error('خطا در برقراری ارتباط با سرور'));
      }}
      className="px-3 py-1.5 rounded-lg bg-green-600 text-white text-xs font-bold hover:bg-green-700"
    >
      ✓ ثبت موجودی
    </button>
    <button
      type="button"
      onClick={() => {
        setEditingStockProduct(null);
        setEditedStock('');
      }}
      className="px-2 py-1.5 rounded-lg bg-slate-200 text-slate-700 text-xs font-bold hover:bg-slate-300"
    >
      لغو
    </button>
  </div>
) : (
  <div className="pt-2 border-t border-slate-100 flex items-center justify-between">
    <span className="text-[10px] text-slate-600">
      موجودی فعلی: <strong className="text-blue-600">{toPersianDigits(product.stock)} عدد</strong>
    </span>
    <button
      type="button"
      onClick={() => {
        setEditingStockProduct(product);
        setEditedStock(product.stock.toString());
      }}
      className="text-green-600 hover:text-green-800 font-bold flex items-center gap-1 bg-green-50 px-2 py-0.5 rounded-md text-[10px]"
    >
      <Package className="w-3 h-3" />
      <span>ویرایش موجودی</span>
    </button>
  </div>
)}
```

#### ج) اضافه کردن import:

در بالای فایل، به imports اضافه کن:

```typescript
import { Package } from 'lucide-react'; // برای آیکون موجودی
```

---

### 2️⃣ Backend: اطمینان از محدودیت (تأیید وضعیت فعلی)

**وضعیت فعلی Backend کاملاً صحیح است:**

✅ فقط دو endpoint برای ویرایش وجود دارد:

1. **PUT /api/v1/supplier/products/:id/stock**
   - فقط فیلد `stock` را آپدیت می‌کند
   - کد: `UPDATE products SET stock = $1, updated_at = NOW() WHERE id = $2`

2. **PUT /api/v1/supplier/products/:id/pricing**
   - فقط فیلد `supplier_base_cost` را آپدیت می‌کند
   - کد: `UPDATE products SET supplier_base_cost = $1, updated_at = NOW() WHERE id = $2`

✅ هیچ endpoint دیگری برای ویرایش محصولات Supplier وجود ندارد.

✅ هیچ endpoint برای ویرایش ویژگی‌ها، تصویر یا سایر فیلدها وجود ندارد.

**نتیجه:** Backend نیازی به تغییر ندارد! ✅

---

### 3️⃣ Context: اطمینان از محدودیت

**فایل:** `src/context/WarehouseContext.tsx`

**وضعیت فعلی:**

```typescript
// خط 217:
const updateWholesaleCost = (productId: string, newCostC: number) => {};
```

این تابع خالی است و هیچ کاری نمی‌کند! ✅

**اگر در آینده نیاز به پیاده‌سازی واقعی شد:**

```typescript
const updateWholesaleCost = async (productId: string, newCostC: number) => {
  try {
    const response = await fetch(`/api/v1/supplier/products/${productId}/pricing`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        new_supplier_base_cost: newCostC,
        reason: 'تغییر قیمت عمده توسط تامین‌کننده'
      })
    });
    
    const data = await response.json();
    
    if (data.success) {
      // به‌روزرسانی local state
      setProducts(prev => 
        prev.map(p => p.id === productId ? {...p, supplierBaseCost: newCostC} : p)
      );
      return true;
    } else {
      throw new Error(data.error || 'خطا در به‌روزرسانی قیمت');
    }
  } catch (err) {
    console.error('Error updating wholesale cost:', err);
    return false;
  }
};

// تابع جدید برای به‌روزرسانی موجودی:
const updateStock = async (productId: string, newStock: number) => {
  try {
    const response = await fetch(`/api/v1/supplier/products/${productId}/stock`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        new_stock: newStock,
        reason: 'تغییر موجودی توسط تامین‌کننده'
      })
    });
    
    const data = await response.json();
    
    if (data.success) {
      setProducts(prev => 
        prev.map(p => p.id === productId ? {...p, stock: newStock} : p)
      );
      return true;
    } else {
      throw new Error(data.error || 'خطا در به‌روزرسانی موجودی');
    }
  } catch (err) {
    console.error('Error updating stock:', err);
    return false;
  }
};
```

**نکته:** Context فعلاً نیازی به تغییر ندارد زیرا UI مستقیماً API را فراخوانی می‌کند.

---

### 4️⃣ مستندسازی: اضافه کردن توضیحات

**فایل جدید:** `docs/SUPPLIER_EDIT_RESTRICTIONS.md`

```markdown
# محدودیت‌های ویرایش محصولات توسط تامین‌کننده

## دلیل محدودیت:

هنگامی که مشتری سفارشی ثبت می‌کند، اطلاعات کامل محصول (عنوان، ویژگی‌ها، تصویر، ...) در سفارش ذخیره می‌شود.

اگر تامین‌کننده بتواند این اطلاعات را تغییر دهد:
- سفارشات قدیمی دیگر معنا ندارند
- پیگیری سفارش غیرممکن می‌شود
- مشتری محصول متفاوتی نسبت به سفارش اولیه دریافت می‌کند

## مثال مشکل:

```
1. Supplier محصول تعریف می‌کند:
   - عنوان: "تی‌شرت مردانه"
   - رنگ: زرد
   - سایز: XL
   - تصویر: yellow-tshirt.jpg

2. مشتری سفارش می‌دهد:
   - Order #123: تی‌شرت زرد XL

3. Supplier ویژگی‌ها را تغییر می‌دهد:
   - رنگ: زرد → قرمز ❌
   - سایز: XL → M ❌

4. نتیجه:
   - سفارش #123 اطلاعات اولیه را ندارد
   - مشتری قرمز M دریافت می‌کند اما زرد XL سفارش داده! 💥
```

## راه‌حل:

### فیلدهای قابل ویرایش: ✅
- **قیمت عمده (C):** قیمت‌ها متغیر هستند و تغییر آن‌ها به سفارشات قبلی آسیب نمی‌زند
- **موجودی:** موجودی لحظه‌ای است و تغییر آن طبیعی است

### فیلدهای غیرقابل ویرایش: ❌
- عنوان، توضیحات، ویژگی‌ها (رنگ، سایز، ...)
- تصویر
- دسته‌بندی، نیچ، بارکد
- گارانتی، انبار، زمان آماده‌سازی
- اصالت کالا

## پیاده‌سازی:

### Backend:
- فقط دو API endpoint برای ویرایش:
  1. `PUT /api/v1/supplier/products/:id/stock`
  2. `PUT /api/v1/supplier/products/:id/pricing`

### Frontend:
- فقط دو دکمه در UI:
  1. "ویرایش قیمت عمده C"
  2. "ویرایش موجودی"

### امنیت:
- هیچ route دیگری برای ویرایش محصولات وجود ندارد
- SQL queries فقط فیلدهای مجاز را آپدیت می‌کنند
```

---

## ✅ خلاصه تغییرات:

### تغییرات مورد نیاز:
1. ✅ **Frontend:** اضافه کردن دکمه و UI ویرایش موجودی
2. ✅ **Context:** (اختیاری) پیاده‌سازی توابع helper
3. ✅ **Docs:** (اختیاری) مستندسازی محدودیت‌ها

### تغییرات غیرضروری:
- ❌ Backend: هیچ تغییری نیاز نیست (قبلاً محدود شده)
- ❌ امنیت: محدودیت‌ها از قبل اعمال شده

---

## 🧪 تست:

بعد از پیاده‌سازی:

1. **تست ویرایش موجودی:**
   - وارد پنل Supplier شو
   - روی یک محصول تأیید شده کلیک کن
   - دکمه "ویرایش موجودی" رو بزن
   - موجودی جدید وارد کن و ثبت کن
   - بررسی کن که موجودی در UI و دیتابیس تغییر کرده

2. **تست ویرایش قیمت:**
   - دکمه "ویرایش قیمت عمده C" رو بزن
   - قیمت جدید وارد کن و ثبت کن
   - بررسی کن که قیمت تغییر کرده

3. **تست محدودیت‌ها:**
   - اطمینان حاصل کن که هیچ دکمه یا فرمی برای ویرایش ویژگی‌ها، تصویر یا سایر فیلدها وجود نداره
   - سعی کن مستقیماً با API request فیلدهای دیگه رو تغییر بدی (باید خطا بده)

4. **تست TypeScript:**
   ```bash
   pnpm exec tsc --noEmit
   ```

5. **تست Build:**
   ```bash
   pnpm run build
   ```

---

## 🎨 نکات طراحی:

- دکمه "ویرایش موجودی" با رنگ سبز (green-600)
- دکمه "ویرایش قیمت" با رنگ آبی (blue-600)
- هر دو دکمه در کنار هم قرار بگیرند
- استایل یکپارچ با بقیه UI
- آیکون مناسب از lucide-react

---

## 📝 نکته امنیتی:

حتی اگر کسی بخواهد با Postman یا ابزارهای دیگه فیلدهای ممنوعه رو تغییر بده، امکان‌پذیر نیست چون:

1. Backend فقط دو endpoint داره
2. SQL queries فقط `stock` و `supplier_base_cost` رو آپدیت می‌کنند
3. هیچ route دیگری برای ویرایش کامل محصول وجود نداره

---

## 🚀 پس از پیاده‌سازی:

```bash
git add .
git commit -m "feat(supplier): Add stock editing UI & enforce product immutability

- Added 'Edit Stock' button next to 'Edit Price' in supplier products
- Only price (C) and stock are editable after product creation
- All other fields (specs, image, title, etc.) are immutable
- Prevents order tracking issues from product modifications
- Backend restrictions already enforced (stock & pricing endpoints only)"
```
