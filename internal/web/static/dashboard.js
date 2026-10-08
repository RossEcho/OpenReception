(function () {
  function initializeDashboard() {
    var element = document.getElementById("business-calendar");
    if (!element) return;
    var loading = element.querySelector(".calendar-loading");
    if (typeof FullCalendar === "undefined") {
      if (loading) loading.textContent = "Calendar library could not be loaded.";
      return;
    }

    try {
      var editor = document.getElementById("appointment-editor");
      var title = document.getElementById("editor-title");
      var meta = document.getElementById("editor-meta");
      var id = document.getElementById("editor-id");
      var date = document.getElementById("editor-date");
      var time = document.getElementById("editor-time");
      var status = document.getElementById("editor-status");
      var editorForm = editor.querySelector("form");
      var pad = function (value) { return String(value).padStart(2, "0"); };
      var localDate = function (value) { return value.getFullYear() + "-" + pad(value.getMonth() + 1) + "-" + pad(value.getDate()); };
      var localTime = function (value) { return pad(value.getHours()) + ":" + pad(value.getMinutes()); };

      var calendar = new FullCalendar.Calendar(element, {
        initialView: window.innerWidth < 600 ? "timeGridDay" : "timeGridWeek",
        firstDay: 0,
        nowIndicator: true,
        allDaySlot: false,
        slotMinTime: "08:00:00",
        slotMaxTime: "21:00:00",
        slotDuration: "00:30:00",
        expandRows: true,
        height: "auto",
        stickyHeaderDates: true,
        dayMaxEvents: true,
        eventMinHeight: 42,
        eventTimeFormat: { hour: "2-digit", minute: "2-digit", hour12: false },
        headerToolbar: { left: "prev,next today", center: "title", right: "dayGridMonth,timeGridWeek,timeGridDay" },
        buttonText: { today: "Today", month: "Month", week: "Week", day: "Day" },
        events: element.dataset.eventsUrl,
        eventClassNames: function (info) { return ["event-" + (info.event.extendedProps.status || "confirmed")]; },
        eventContent: function (info) {
          var props = info.event.extendedProps;
          var frame = document.createElement("div");
          frame.className = "calendar-event-card";
          var clock = document.createElement("span");
          clock.className = "event-clock";
          clock.textContent = info.timeText;
          var name = document.createElement("strong");
          name.textContent = props.status === "owner_blocked" ? "Owner time" : (props.customer || "Appointment");
          var service = document.createElement("small");
          service.textContent = props.service || "Reserved";
          frame.append(clock, name, service);
          return { domNodes: [frame] };
        },
        eventClick: function (info) {
          var props = info.event.extendedProps;
          id.value = info.event.id;
          date.value = localDate(info.event.start);
          time.value = localTime(info.event.start);
          status.value = props.status === "owner_blocked" ? "confirmed" : props.status;
          title.textContent = props.status === "owner_blocked" ? "Owner time" : (props.customer || "Appointment");
          meta.textContent = [props.service, props.phone, props.duration ? props.duration + " minutes" : "", props.note].filter(Boolean).join(" · ");
          editorForm.hidden = props.status === "owner_blocked";
          editor.hidden = false;
          editor.scrollIntoView({ behavior: "smooth", block: "nearest" });
        },
        windowResize: function (info) {
          if (window.innerWidth < 600 && info.view.type !== "timeGridDay") calendar.changeView("timeGridDay");
        }
      });
      calendar.render();
    } catch (error) {
      element.innerHTML = '<div class="calendar-loading calendar-error">Calendar failed to initialize.</div>';
      console.error(error);
    }
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", initializeDashboard);
  else initializeDashboard();
})();
