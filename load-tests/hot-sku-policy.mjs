export function evaluateHotSKU(s) {
 const failed=[];
 if(s.contenders!==100||s.paid!==5||s.conflicts!==95)failed.push('five_winners_ninety_five_explicit_conflicts');
 if(s.wrongAmount!==0||s.failures!==0)failed.push('amount_or_response_assertion');
 if(s.duplicateCallbacks!==10)failed.push('duplicate_callback_replay');
 return failed;
}
