// Define a global array to store markers for later manipulation
var markerArray = [];

// Define some global variables for our map layers
var layer_USGS = L.layerGroup();
var layer_bgeigic = L.layerGroup();
var layer_OpenTopo = L.layerGroup();
var layer_MapQuestOpen_Aerial = L.layerGroup();

// Set default coordinates and zoom level
var init_lat = 37.698713;
var init_lng = -122.478335;
var init_zoom = 13;

// Define color gradient values for low to high measurements
var lowval = 0.10;
var highval = 10.00;

// Array to hold data points from API response
var geigerData = [];

// Initialize leaflet map
var map = new L.Map('map', {
  center: new L.LatLng(init_lat, init_lng),
  zoom: init_zoom,
  attributionControl: false
});

// Add zoom control to the map
map.addControl(new L.Control.Zoom({
  position: 'bottomright'
}));

// Add scale control to the map
map.addControl(new L.Control.Scale({
  position: 'bottomleft',
  imperial: false
}));

// Add the tile layers we want the user to switch between
var baseLayers = {
  "USGS Topographic": layer_USGS,
  "BGEIGIC": layer_MapQuestOpen_Aerial,
  "OpenTopoMap": layer_OpenTopo
};

layer_USGS.addTo(map);

L.control.layers(baseLayers).addTo(map);

// Function to update the display based on filter controls
function update_display() {
  var lowerlimit = document.getElementById("lowvalue").value;
  var upperlimit = document.getElementById("highvalue").value;

  for (i = 0; i < markerArray.length; i++) {
    // Determine opacity value based on measurement
    var opac = markerArray[i].options.val;

    if (opac > highval) {
      opac = .90;
    } else if (opac < lowval) {
      opac = .10;
    } else {
      opac = (Math.log(opac / lowval)) / (Math.log(highval / lowval) * 10);
    }

    // Get the current marker's value
    var mval = markerArray[i].options.val;

    // Check that marker is within range of our filters
    if (mval >= lowerlimit && mval <= upperlimit) {
      // Set new opacity based on value
      markerArray[i].setOpacity(opac);
    } else {
      // Hide marker by setting opacity to 0
      markerArray[i].setOpacity(0);
    }
  }
}

// Function to convert UNIX timestamp to readable format
function timeConverter(UNIX_timestamp) {
  var a = new Date(UNIX_timestamp * 1000);
  var months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  var year = a.getFullYear();
  var month = months[a.getMonth()];
  var date = a.getDate();
  var hour = a.getHours();
  var min = a.getMinutes();
  var sec = a.getSeconds();
  var time = date + ' ' + month + ' ' + year + ' ' + hour + ':' + min + ':' + sec;
  return time;
}

// Create a custom icon for our markers
var geigerIcon = L.icon({
  iconUrl: '/img/geiger.png',
  iconSize: [24, 24],
  iconAnchor: [12, 12]
});

// Function to clear existing markers from the map
function clearMarkers() {
  for (i = 0; i < markerArray.length; i++) {
    map.removeLayer(markerArray[i]);
  }
  markerArray.length = 0;
}

// Function to add markers to the map based on selected data type
function addMarkers(type) {
  clearMarkers();

  // Define API endpoints for different data types
  var url = '';
  switch (type) {
    case 'air':
      url = 'https://api.safecast.org/en-US/devices/4734/measurements.json';
      break;
    case 'water':
      url = 'https://api.safecast.org/en-US/devices/4735/measurements.json';
      break;
    case 'solid':
      url = 'https://api.safecast.org/en-US/devices/4736/measurements.json';
      break;
  }

  // Fetch data from the API
  $.ajax({
    url: url,
    dataType: 'json',
    success: function(data) {
      geigerData = data;
      
      // Process each data point and add markers to the map
      $.each(data, function(index, value) {
        // Calculate the size of the circle based on the measurement value
        var csize = Math.min(Math.max(value.value / 10, 2), 20); // Constrain size between 2 and 20
        
        // Create popup content with measurement details
        var popup = "<div class='popup'>";
        popup += "<h3>" + value.value.toFixed(6) + "</h3>";
        popup += "<p>Date: " + timeConverter(value.recorded_at) + "</p>";
        popup += "<p>Latitude: " + value.latitude + "</p>";
        popup += "<p>Longitude: " + value.longitude + "</p>";
        popup += "<p>Device ID: " + value.device_id + "</p>";
        popup += "<p>Unit: " + value.unit + "</p>";
        popup += "</div>";

        // Determine color based on measurement value using a logarithmic scale
        var col;
        var opacityValue = (Math.log(value.value) - Math.log(lowval)) / (Math.log(highval) - Math.log(lowval));
        opacityValue = Math.min(Math.max(opacityValue, 0), 1); // Clamp between 0 and 1

        if (value.value <= 0.1) {
          col = '#00FF00'; // Green for low values
        } else if (value.value <= 1.0) {
          col = '#FFFF00'; // Yellow for medium values
        } else {
          col = '#FF0000'; // Red for high values
        }

        // Create circular marker with calculated properties
        var marker = L.circleMarker([value.latitude, value.longitude], {
          radius: csize,
          color: col,
          fillColor: col,
          fillOpacity: opacityValue,
          val: value.value,
          bUseSimpleInClick: true
        });

        // Add popup and click event handling
        marker.bindPopup(popup);
        marker.on('click', function(e) {
          this.openPopup();
        });

        // Add marker to the map and marker array
        marker.addTo(map);
        markerArray.push(marker);
      });
    },
    error: function(error) {
      console.error('Error fetching data:', error);
    }
  });
}

// Initialize the map with air measurements by default
addMarkers('air');

// Event listeners for the measurement type buttons
document.getElementById('air').addEventListener('click', function() {
  addMarkers('air');
});

document.getElementById('water').addEventListener('click', function() {
  addMarkers('water');
});

document.getElementById('solid').addEventListener('click', function() {
  addMarkers('solid');
});