package navapp.crash

import android.content.Context
import android.os.Bundle
import android.view.View
import androidx.fragment.app.Fragment
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import kotlinx.coroutines.launch

// Crashes (traces in testdata/navapp/crash): Money.format, which two screens and a worker use,
// throws. The deepest own frame reaches all three; the coroutine of InvoiceViewModel above it in
// the trace reaches only InvoiceFragment, so the crash is there, though no frame names the screen.

object Money {
    fun format(cents: Long): String {
        require(cents >= 0) { "negative amount: $cents" }
        return "${cents / 100}.${cents % 100}"
    }
}

class InvoiceViewModel : ViewModel() {
    var total = ""

    fun load(cents: Long) {
        viewModelScope.launch { total = Money.format(cents) }
    }
}

class InvoiceFragment : Fragment() {
    private val viewModel = InvoiceViewModel()

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) = viewModel.load(-1)
}

class ReceiptFragment : Fragment() {
    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        view.contentDescription = Money.format(5)
    }
}

class ReportWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        Money.format(-1)
        return Result.success()
    }
}
